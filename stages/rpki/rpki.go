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
	rpki_remove          // Remove invalid prefixes from the reachable prefixes
	rpki_tag             // Tag message but pass through unchanged
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

	// config
	invalid int
	strict  bool
	rtr     string
	file    string

	// current ROA cache
	roaReady chan bool           // if closed, ROA cache is ready for use
	roa4     atomic.Pointer[ROA] // IPv4
	roa6     atomic.Pointer[ROA] // IPv6
	next4    ROA                 // next roa4 (pending apply)
	next6    ROA                 // next roa6 (pending apply)

	// file watcher state
	fileMod  time.Time
	fileHash [32]byte

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
		roaReady:  make(chan bool),
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
	f.Bool("rtr-tls", false, "use TLS for RTR connection")
	f.Bool("insecure", false, "do not validate TLS certificates")
	f.String("file", "", "use a ROA file instead of RTR (JSON/CSV, auto-reloaded)")
	f.String("invalid", "withdraw", "action for INVALID prefixes: withdraw|drop|tag|remove")
	f.Bool("strict", false, "treat NOT_FOUND same as INVALID")

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
	case "tag":
		s.invalid = rpki_tag
	case "remove":
		s.invalid = rpki_remove
	default:
		return fmt.Errorf("--invalid must be withdraw, drop, tag, or remove")
	}

	s.strict = k.Bool("strict")
	s.rtr = k.String("rtr")
	s.file = k.String("file")

	// need at least one source
	if s.rtr == "" && s.file == "" {
		return fmt.Errorf("must specify --rtr or --file")
	}

	// Register callback for UPDATE messages
	s.P.OnMsg(s.validate, s.Dir, msg.UPDATE)

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

	// block until the ROA cache is ready
	select {
	case <-s.roaReady:
		return nil
	case <-s.Ctx.Done():
		return s.Ctx.Err()
	}
}

func (s *Rpki) Stop() error {
	s.rtr_mu.Lock()
	if s.rtr_conn != nil {
		s.Debug().Msg("closing RTR connection")
		s.rtr_conn.Close()
	}
	s.rtr_mu.Unlock()
	return nil
}
