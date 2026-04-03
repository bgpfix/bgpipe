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
	min_vrp_v4 = 8  // no VRPs shorter than /8 for IPv4
	min_vrp_v6 = 12 // no VRPs shorter than /12 for IPv6
)

// ROV validation status
const (
	rov_valid     = iota // prefix+origin covered by valid VRP
	rov_invalid          // prefix+origin conflicts with VRP
	rov_not_found        // no VRP covers this prefix
)

// ASPA path validation status
const (
	aspa_valid   = iota // path is valley-free and fully attested
	aspa_unknown        // insufficient attestation
	aspa_invalid        // proven route leak
)

// action for invalid prefixes/paths
const (
	act_withdraw = iota // move invalid prefixes to withdrawn
	act_drop            // drop entire UPDATE message
	act_filter          // remove invalid prefixes silently (ROV only)
	act_split           // split invalid prefixes to separate UPDATE (ROV only)
	act_keep            // keep unchanged (tag only)
)

// VRP represents a single Validated ROA Payload
type VRP struct {
	MaxLen uint8
	ASN    uint32
}

// VRPs maps prefixes to lists of VRP entries
type VRPs = map[netip.Prefix][]VRP

// ASPA maps Customer ASN to its list of Provider ASNs
type ASPA = map[uint32][]uint32

// Rpki validates BGP UPDATE messages using RPKI data (ROV + ASPA)
type Rpki struct {
	*core.StageBase
	split *pipe.Input // used for --invalid=split

	// ROV config
	rtr     string
	file    string
	rov_act int
	strict  bool
	tag     bool
	event   string

	// ASPA config (requires --aspa)
	aspa_on   bool   // true if --aspa flag is set
	aspa_act  int    // action for ASPA INVALID paths
	aspa_tag  bool   // add aspa/status tag
	aspa_ev   string // emit event on ASPA INVALID
	aspa_role string // --aspa-role flag value

	// resolved peer role (per-direction, set once on first UPDATE per dir)
	peer_role    [2]int       // caps.ROLE_* constant; -1 = unresolved
	peer_role_mu [2]sync.Once
	peer_role_ok [2]bool     // true if resolved successfully
	peer_down    [2]bool     // true if peer is provider/RS (downstream path)

	// VRP cache (current = atomic pointer; next = pending)
	vrp_done chan bool
	vrp4     atomic.Pointer[VRPs]
	vrp6     atomic.Pointer[VRPs]
	next4    VRPs
	next6    VRPs

	// ASPA cache
	aspa      atomic.Pointer[ASPA]
	next_aspa ASPA

	// prometheus metrics
	cnt_msg        *metrics.Counter
	cnt_rov_valid  *metrics.Counter
	cnt_rov_inv    *metrics.Counter
	cnt_rov_nf     *metrics.Counter
	cnt_aspa_valid *metrics.Counter
	cnt_aspa_unk   *metrics.Counter
	cnt_aspa_inv   *metrics.Counter

	// file watcher state
	file_mod  time.Time
	file_hash [32]byte

	// RTR client state (protected by rtr_mu)
	rtr_mu    sync.Mutex
	rtr_conn  net.Conn
	rtr_valid bool
}

func NewRpki(parent *core.StageBase) core.Stage {
	s := &Rpki{
		StageBase: parent,
		vrp_done:  make(chan bool),
		peer_role: [2]int{-1, -1},
	}

	s.vrp4.Store(new(VRPs))
	s.vrp6.Store(new(VRPs))
	s.aspa.Store(new(ASPA))
	s.nextFlush()

	o := &s.Options
	o.Descr = "validate UPDATEs using RPKI (ROV; use --aspa for ASPA)"
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
	f.Bool("no-ipv6", false, "avoid IPv6 for RTR server connection")
	f.String("file", "", "use a VRP/ASPA file instead of RTR (JSON/CSV, auto-reloaded)")
	f.String("invalid", "withdraw", "action for ROV INVALID: withdraw|filter|drop|split|keep")
	f.Bool("strict", false, "treat NOT_FOUND same as INVALID")
	f.Bool("tag", true, "add ROV validation status to message tags")
	f.String("event", "", "emit event on ROV INVALID messages")
	f.Bool("no-wait", false, "start before VRP/ASPA cache is ready")
	f.Bool("aspa", false, "enable ASPA path validation (draft-ietf-sidrops-aspa-verification)")
	f.String("aspa-invalid", "withdraw", "action for ASPA INVALID: withdraw|drop|keep")
	f.Bool("aspa-tag", true, "add ASPA validation status to message tags")
	f.String("aspa-event", "", "emit event on ASPA INVALID paths")
	f.String("aspa-role", "auto", "peer BGP role: auto|provider|customer|peer|rs|rs-client")

	return s
}

func (s *Rpki) Attach() error {
	k := s.K

	// prometheus metrics
	prefix := s.MetricPrefix()
	s.cnt_msg = metrics.GetOrCreateCounter(prefix + "messages_total")
	s.cnt_rov_valid = metrics.GetOrCreateCounter(prefix + "rov_valid_total")
	s.cnt_rov_inv = metrics.GetOrCreateCounter(prefix + "rov_invalid_total")
	s.cnt_rov_nf = metrics.GetOrCreateCounter(prefix + "rov_not_found_total")
	metrics.NewGauge(prefix+"vrps_ipv4", func() float64 {
		if v := s.vrp4.Load(); v != nil {
			return float64(len(*v))
		}
		return 0
	})
	metrics.NewGauge(prefix+"vrps_ipv6", func() float64 {
		if v := s.vrp6.Load(); v != nil {
			return float64(len(*v))
		}
		return 0
	})

	s.aspa_on = k.Bool("aspa")
	if s.aspa_on {
		s.cnt_aspa_valid = metrics.GetOrCreateCounter(prefix + "aspa_valid_total")
		s.cnt_aspa_unk = metrics.GetOrCreateCounter(prefix + "aspa_unknown_total")
		s.cnt_aspa_inv = metrics.GetOrCreateCounter(prefix + "aspa_invalid_total")
		metrics.NewGauge(prefix+"aspa_entries", func() float64 {
			if a := s.aspa.Load(); a != nil {
				return float64(len(*a))
			}
			return 0
		})
	}

	// parse ROV action
	switch strings.ToLower(k.String("invalid")) {
	case "withdraw":
		s.rov_act = act_withdraw
	case "drop":
		s.rov_act = act_drop
	case "filter":
		s.rov_act = act_filter
	case "split":
		s.rov_act = act_split
	case "keep":
		s.rov_act = act_keep
	default:
		return fmt.Errorf("--invalid must be withdraw, filter, drop, split or keep")
	}

	// parse ASPA config
	if s.aspa_on {
		switch strings.ToLower(k.String("aspa-invalid")) {
		case "withdraw":
			s.aspa_act = act_withdraw
		case "drop":
			s.aspa_act = act_drop
		case "keep":
			s.aspa_act = act_keep
		default:
			return fmt.Errorf("--aspa-invalid must be withdraw, drop or keep")
		}
		s.aspa_tag = k.Bool("aspa-tag")
		s.aspa_ev = k.String("aspa-event")
		s.aspa_role = strings.ToLower(k.String("aspa-role"))
		if s.aspa_role != "auto" {
			if _, ok := parseRoleName(s.aspa_role); !ok {
				return fmt.Errorf("--aspa-role must be auto, provider, customer, peer, rs or rs-client")
			}
		}
	}

	s.strict = k.Bool("strict")
	s.rtr = k.String("rtr")
	s.file = k.String("file")
	s.tag = k.Bool("tag")
	s.event = k.String("event")

	if s.rtr == "" && s.file == "" {
		return fmt.Errorf("must specify --rtr or --file")
	}

	s.P.OnMsg(s.validateMsg, s.Dir, msg.UPDATE)

	if s.rov_act == act_split {
		s.split = s.P.AddInput(s.Dir)
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

	if !s.K.Bool("no-wait") {
		select {
		case <-s.vrp_done:
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
		source := "rtr"
		if s.file != "" {
			source = "file"
		}

		met := map[string]uint64{
			"messages":      s.cnt_msg.Get(),
			"rov_valid":     s.cnt_rov_valid.Get(),
			"rov_invalid":   s.cnt_rov_inv.Get(),
			"rov_not_found": s.cnt_rov_nf.Get(),
		}

		resp := map[string]any{
			"source":  source,
			"metrics": met,
		}

		if v := s.vrp4.Load(); v != nil {
			resp["vrps_ipv4"] = len(*v)
		}
		if v := s.vrp6.Load(); v != nil {
			resp["vrps_ipv6"] = len(*v)
		}

		if s.aspa_on {
			if a := s.aspa.Load(); a != nil {
				resp["aspa_entries"] = len(*a)
			}
			met["aspa_valid"] = s.cnt_aspa_valid.Get()
			met["aspa_unknown"] = s.cnt_aspa_unk.Get()
			met["aspa_invalid"] = s.cnt_aspa_inv.Get()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	return nil
}
