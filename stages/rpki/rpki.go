package rpki

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	rtrlib "github.com/bgp/stayrtr/lib"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

const (
	minROALenV4 = 8  // No ROAs shorter than /8 for IPv4
	minROALenV6 = 12 // No ROAs shorter than /12 for IPv6
)

// RPKI validation status
const (
	rpki_valid     = iota // Prefix+origin covered by valid ROA
	rpki_invalid          // Prefix+origin conflicts with ROA
	rpki_not_found        // No ROA covers this prefix
)

// what to do with invalid prefixes
const (
	rpki_withdraw = iota // Move invalid prefixes to withdrawn (RFC 7606)
	rpki_drop            // Drop entire UPDATE if any prefix invalid
	rpki_filter          // Remove invalid prefixes from the reachable prefixes
	rpki_split           // Split invalid prefixes into separate UPDATE withdrawing them
	rpki_ignore          // Keep invalid prefixes unchanged
)

// ROAEntry represents a single VRP (Validated ROA Payload)
type ROAEntry struct {
	MaxLen uint8
	ASN    uint32
}

// ROA maps prefixes to lists of ROA entries
type ROA = map[netip.Prefix][]ROAEntry

// Rpki is a stage that validates BGP UPDATE messages using RPKI data
type Rpki struct {
	*core.StageBase
	in_split *pipe.Input // used for --invalid=split

	// config
	rtr     string
	file    string
	invalid int
	strict  bool
	tag     bool
	event   string

	// current ROA cache
	roa_done chan bool           // is ROA cache ready for use?
	roa4     atomic.Pointer[ROA] // current IPv4 ROA
	roa6     atomic.Pointer[ROA] // current IPv6 ROA
	next4    ROA                 // next roa4 (pending apply)
	next6    ROA                 // next roa6 (pending apply)

	// file watcher state
	file_mod  time.Time // last modification time
	file_hash [32]byte  // last file hash

	// RTR client state
	rtr_mu     sync.Mutex
	rtr_conn   net.Conn              // RTR connection
	rtr_client *rtrlib.ClientSession // RTR client
	rtr_sessid uint16                // last session ID from server
	rtr_serial uint32                // last serial number from server
	rtr_valid  bool                  // true if we have a valid serial to use
}

func NewRpki(parent *core.StageBase) core.Stage {
	s := &Rpki{
		StageBase: parent,
		roa_done:  make(chan bool),
	}

	s.roa4.Store(new(ROA))
	s.roa6.Store(new(ROA))
	s.nextFlush()

	o := &s.Options
	o.Descr = "validate UPDATEs using RPKI"
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
	f.String("file", "", "use a ROA file instead of RTR (JSON/CSV, auto-reloaded)")
	f.String("invalid", "withdraw", "action for INVALID prefixes: withdraw|filter|drop|split|ignore")
	f.Bool("strict", false, "treat NOT_FOUND same as INVALID")
	f.Bool("tag", true, "add RPKI validation status to message tags")
	f.String("event", "", "emit event on RPKI INVALID messages")
	f.Bool("asap", false, "do not wait for ROA cache to become ready")

	return s
}

func (s *Rpki) Attach() error {
	k := s.K

	// Parse invalid action
	switch strings.ToLower(k.String("invalid")) {
	case "withdraw":
		s.invalid = rpki_withdraw
	case "drop":
		s.invalid = rpki_drop
	case "filter":
		s.invalid = rpki_filter
	case "split":
		s.invalid = rpki_split
	case "ignore":
		s.invalid = rpki_ignore
	default:
		return fmt.Errorf("--invalid must be withdraw, filter, drop, split or ignore")
	}

	s.strict = k.Bool("strict")
	s.rtr = k.String("rtr")
	s.file = k.String("file")
	s.tag = k.Bool("tag")
	s.event = k.String("event")

	// need at least one source
	if s.rtr == "" && s.file == "" {
		return fmt.Errorf("must specify --rtr or --file")
	}

	// register callback for UPDATE messages
	s.P.OnMsg(s.validateMsg, s.Dir, msg.UPDATE)

	// create input for --invalid=split
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

	// block until the ROA cache is ready?
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
