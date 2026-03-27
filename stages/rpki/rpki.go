package rpki

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/go-chi/chi/v5"
)

const (
	minROALenV4 = 8  // no ROAs shorter than /8 for IPv4
	minROALenV6 = 12 // no ROAs shorter than /12 for IPv6
)

// RPKI ROV validation status
const (
	rpki_valid     = iota // prefix+origin covered by valid ROA
	rpki_invalid          // prefix+origin conflicts with ROA
	rpki_not_found        // no ROA covers this prefix
)

// ASPA path validation status
const (
	aspa_valid   = iota // path is valley-free and fully attested
	aspa_unknown        // insufficient attestation (treated same as valid for policy)
	aspa_invalid        // proven route leak
)

// what to do with invalid prefixes/paths
const (
	rpki_withdraw = iota // move invalid prefixes to withdrawn (RFC 7606)
	rpki_drop            // drop entire UPDATE if any prefix/path invalid
	rpki_filter          // remove invalid prefixes from reachable list
	rpki_split           // split invalid prefixes into separate withdrawing UPDATE
	rpki_keep            // keep invalid prefixes unchanged
)

// ROAEntry represents a single VRP (Validated ROA Payload)
type ROAEntry struct {
	MaxLen uint8
	ASN    uint32
}

// ROA maps prefixes to lists of ROA entries
type ROA = map[netip.Prefix][]ROAEntry

// ASPA maps Customer ASN to its list of Provider ASNs
type ASPA = map[uint32][]uint32

// Rpki is a stage that validates BGP UPDATE messages using RPKI data (ROV + ASPA)
type Rpki struct {
	*core.StageBase
	in_split *pipe.Input // used for --invalid=split

	// ROV config
	rtr     string
	file    string
	invalid int
	strict  bool
	tag     bool
	event   string

	// ASPA config
	aspa_action int    // what to do with ASPA INVALID paths (same constants as invalid)
	aspa_tag    bool   // add aspa/status tag to messages
	aspa_event  string // emit event on ASPA INVALID paths
	role_name   string // --role flag value ("auto", "provider", "customer", etc.)

	// resolved peer role (set once on first UPDATE)
	peer_role     int        // resolved peer role as caps.ROLE_* constant; -1 = unresolved
	peer_role_mu  sync.Once  // ensures role is resolved exactly once
	peer_role_ok  bool       // true if role was successfully resolved
	peer_downstream bool     // true if peer is our provider (downstream path)

	// ROA cache (current = atomic pointer; next = pending apply under rtr_mu)
	roa_done chan bool
	roa4     atomic.Pointer[ROA]
	roa6     atomic.Pointer[ROA]
	next4    ROA
	next6    ROA

	// ASPA cache (same pattern as ROA)
	aspa     atomic.Pointer[ASPA]
	nextAspa ASPA

	// prometheus metrics
	cMessages   *metrics.Counter // bgpipe_rpki_messages_total
	cValid      *metrics.Counter // bgpipe_rpki_valid_total
	cInvalid    *metrics.Counter // bgpipe_rpki_invalid_total
	cNotFound   *metrics.Counter // bgpipe_rpki_not_found_total
	cAspaValid  *metrics.Counter // bgpipe_rpki_aspa_valid_total
	cAspaUnknown *metrics.Counter // bgpipe_rpki_aspa_unknown_total
	cAspaInvalid *metrics.Counter // bgpipe_rpki_aspa_invalid_total

	// file watcher state
	file_mod  time.Time
	file_hash [32]byte

	// RTR client state (protected by rtr_mu)
	rtr_mu     sync.Mutex
	rtr_conn   net.Conn // current RTR connection
	rtr_sessid uint16   // last session ID
	rtr_serial uint32   // last serial number
	rtr_valid  bool     // true if we have a valid serial
}

func NewRpki(parent *core.StageBase) core.Stage {
	s := &Rpki{
		StageBase: parent,
		roa_done:  make(chan bool),
		peer_role: -1, // unresolved
	}

	s.roa4.Store(new(ROA))
	s.roa6.Store(new(ROA))
	s.aspa.Store(new(ASPA))
	s.nextFlush()

	o := &s.Options
	o.Descr = "validate UPDATEs using RPKI (ROV + ASPA)"
	o.FilterIn = true
	o.Bidir = true

	f := o.Flags
	f.String("rtr", "rtr.rpki.cloudflare.com:8282", "RTR server address (host:port)")
	f.Duration("rtr-refresh", time.Hour, "RTR refresh interval")
	f.Duration("rtr-retry", 10*time.Minute, "RTR retry interval")
	f.Duration("timeout", time.Second*15, "connect timeout (0 means none)")
	f.Bool("retry", true, "retry connection on temporary errors")
	f.Int("retry-max", 0, "maximum number of connection retries (0 means unlimited)")
	f.Bool("tls", false, "connect over TLS")
	f.Bool("insecure", false, "do not validate TLS certificates")
	f.Bool("no-ipv6", false, "avoid IPv6 if possible")
	f.String("file", "", "use a ROA/ASPA file instead of RTR (JSON/CSV, auto-reloaded)")
	f.String("invalid", "withdraw", "action for ROV INVALID: withdraw|filter|drop|split|keep")
	f.Bool("strict", false, "treat NOT_FOUND same as INVALID")
	f.Bool("tag", true, "add RPKI validation status to message tags")
	f.String("event", "", "emit event on ROV INVALID messages")
	f.Bool("asap", false, "do not wait for cache to become ready")
	f.String("aspa-invalid", "keep", "action for ASPA INVALID paths: withdraw|filter|drop|split|keep")
	f.Bool("aspa-tag", true, "add ASPA validation status to message tags")
	f.String("aspa-event", "", "emit event on ASPA INVALID messages")
	f.String("role", "auto", "peer BGP role for ASPA: auto|provider|customer|peer|rs|rs-client")

	return s
}

func (s *Rpki) Attach() error {
	k := s.K

	// prometheus counters
	prefix := s.MetricPrefix()
	s.cMessages = metrics.GetOrCreateCounter(prefix + "messages_total")
	s.cValid = metrics.GetOrCreateCounter(prefix + "valid_total")
	s.cInvalid = metrics.GetOrCreateCounter(prefix + "invalid_total")
	s.cNotFound = metrics.GetOrCreateCounter(prefix + "not_found_total")
	s.cAspaValid = metrics.GetOrCreateCounter(prefix + "aspa_valid_total")
	s.cAspaUnknown = metrics.GetOrCreateCounter(prefix + "aspa_unknown_total")
	s.cAspaInvalid = metrics.GetOrCreateCounter(prefix + "aspa_invalid_total")
	metrics.NewGauge(prefix+"roa4_prefixes", func() float64 {
		if r4 := s.roa4.Load(); r4 != nil {
			return float64(len(*r4))
		}
		return 0
	})
	metrics.NewGauge(prefix+"roa6_prefixes", func() float64 {
		if r6 := s.roa6.Load(); r6 != nil {
			return float64(len(*r6))
		}
		return 0
	})
	metrics.NewGauge(prefix+"aspa_entries", func() float64 {
		if a := s.aspa.Load(); a != nil {
			return float64(len(*a))
		}
		return 0
	})

	// parse ROV action
	switch strings.ToLower(k.String("invalid")) {
	case "withdraw":
		s.invalid = rpki_withdraw
	case "drop":
		s.invalid = rpki_drop
	case "filter":
		s.invalid = rpki_filter
	case "split":
		s.invalid = rpki_split
	case "keep":
		s.invalid = rpki_keep
	default:
		return fmt.Errorf("--invalid must be withdraw, filter, drop, split or keep")
	}

	// parse ASPA action
	switch strings.ToLower(k.String("aspa-invalid")) {
	case "withdraw":
		s.aspa_action = rpki_withdraw
	case "drop":
		s.aspa_action = rpki_drop
	case "filter":
		s.aspa_action = rpki_filter
	case "split":
		s.aspa_action = rpki_split
	case "keep":
		s.aspa_action = rpki_keep
	default:
		return fmt.Errorf("--aspa-invalid must be withdraw, filter, drop, split or keep")
	}

	s.strict = k.Bool("strict")
	s.rtr = k.String("rtr")
	s.file = k.String("file")
	s.tag = k.Bool("tag")
	s.event = k.String("event")
	s.aspa_tag = k.Bool("aspa-tag")
	s.aspa_event = k.String("aspa-event")
	s.role_name = k.String("role")

	if s.rtr == "" && s.file == "" {
		return fmt.Errorf("must specify --rtr or --file")
	}

	s.P.OnMsg(s.validateMsg, s.Dir, msg.UPDATE)

	if s.invalid == rpki_split {
		s.in_split = s.P.AddInput(s.Dir)
	}

	return nil
}

func (s *Rpki) Prepare() error {
	switch {
	case s.file != "":
		go s.fileRun()
	case s.rtr != "":
		go s.rtrRun()
	default:
		panic("no RPKI source configured")
	}

	if !s.K.Bool("asap") {
		select {
		case <-s.roa_done:
		case <-s.Ctx.Done():
		}
	}

	return nil
}

func (s *Rpki) Stop() error {
	s.rtr_mu.Lock()
	if s.rtr_conn != nil {
		s.rtr_conn.Close()
	}
	s.rtr_mu.Unlock()
	return nil
}

func (s *Rpki) RouteHTTP(r chi.Router) error {
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		var roa4size, roa6size, aspaSize int
		if r4 := s.roa4.Load(); r4 != nil {
			roa4size = len(*r4)
		}
		if r6 := s.roa6.Load(); r6 != nil {
			roa6size = len(*r6)
		}
		if a := s.aspa.Load(); a != nil {
			aspaSize = len(*a)
		}

		source := "rtr"
		if s.file != "" {
			source = "file"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"source": source,
			"roa4":   roa4size,
			"roa6":   roa6size,
			"aspa":   aspaSize,
			"metrics": map[string]uint64{
				"messages":     s.cMessages.Get(),
				"valid":        s.cValid.Get(),
				"invalid":      s.cInvalid.Get(),
				"not_found":    s.cNotFound.Get(),
				"aspa_valid":   s.cAspaValid.Get(),
				"aspa_unknown": s.cAspaUnknown.Get(),
				"aspa_invalid": s.cAspaInvalid.Get(),
			},
		})
	})
	return nil
}
