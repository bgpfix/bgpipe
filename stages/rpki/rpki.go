/*
 * rpki: real-time RPKI validation of BGP UPDATE messages
 *
 * Maintains an in-memory ROA cache populated via RTR protocol (primary)
 * or JSON/CSV file monitoring (backup).
 *
 * License: MIT
 * Author: Pawel Foremski <pjf@foremski.pl>
 */

package rpki

import (
	"fmt"
	"net/netip"
	"strings"
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

const (
	RPKI_VALID     = iota // Prefix+origin covered by valid ROA
	RPKI_INVALID          // Prefix+origin conflicts with ROA
	RPKI_NOT_FOUND        // No ROA covers this prefix
)

// Action for invalid prefixes
const (
	RPKI_WITHDRAW = iota // Move invalid prefixes to withdrawn (RFC 7606)
	RPKI_DROP            // Drop entire UPDATE if any prefix invalid
	RPKI_REMOVE          // Remove invalid prefixes from the reachable prefixes
	RPKI_TAG             // Tag message but pass through unchanged
)

// ROAEntry represents a single VRP (Validated ROA Payload)
type ROAEntry struct {
	MaxLen uint8
	ASN    uint32
}

type ROAS = map[netip.Prefix][]ROAEntry

type Rpki struct {
	*core.StageBase

	// Config
	invalidAction int
	strict        bool
	rtrAddr       string
	rtrTLS        bool
	filePath      string
	filePoll      time.Duration
	readyTimeout  time.Duration

	// ROA cache (atomic pointers, readers just Load() and read)
	roa4 atomic.Pointer[ROAS]
	roa6 atomic.Pointer[ROAS]

	// File watcher state
	fileLastMod  time.Time
	fileLastHash [32]byte

	// RTR client state
	rtrSession *rtrlib.ClientSession // RTR session
	rtrUpdate  chan error            // signals cache update
	next4      ROAS                  // pending VRP additions
	next6      ROAS                  // pending VRP additions
}

func NewRpki(parent *core.StageBase) core.Stage {
	s := &Rpki{
		StageBase: parent,
		rtrUpdate: make(chan error, 1),
	}
	o := &s.Options
	f := o.Flags

	o.Descr = "validate UPDATEs using RPKI"
	o.FilterIn = true
	o.Bidir = true

	// RTR Configuration
	f.String("rtr", "", "RTR server address (host:port)")
	f.Bool("rtr-tls", false, "use TLS for RTR connection")
	f.Bool("rtr-insecure", false, "do not valid RTR TLS certificate")
	f.Duration("rtr-refresh", time.Hour, "RTR refresh interval")
	f.Duration("rtr-retry", 5*time.Minute, "RTR retry interval")
	f.Duration("rtr-expire", 2*time.Hour, "RTR expire interval")

	// File Configuration
	f.String("file", "", "ROA file path (JSON/CSV), auto-reloaded on changes")
	f.Duration("file-poll", 30*time.Second, "file modification check interval")

	// Validation Behavior
	f.String("invalid", "withdraw", "action for INVALID prefixes: withdraw|drop|tag|remove")
	f.Bool("strict", false, "treat NOT_FOUND same as INVALID")

	// Operational
	f.Duration("ready-timeout", 60*time.Second, "timeout waiting for ROA cache")

	return s
}

func (s *Rpki) Attach() error {
	k := s.K

	// Parse invalid action
	switch strings.ToLower(k.String("invalid")) {
	case "withdraw":
		s.invalidAction = RPKI_WITHDRAW
	case "drop":
		s.invalidAction = RPKI_DROP
	case "tag":
		s.invalidAction = RPKI_TAG
	case "remove":
		s.invalidAction = RPKI_REMOVE
	default:
		return fmt.Errorf("--invalid must be withdraw, drop, tag, or remove")
	}

	s.strict = k.Bool("strict")

	// Source config
	s.rtrAddr = k.String("rtr")
	s.rtrTLS = k.Bool("rtr-tls")
	s.filePath = k.String("file")
	s.filePoll = k.Duration("file-poll")
	s.readyTimeout = k.Duration("ready-timeout")

	// Validate: need at least one source
	if s.rtrAddr == "" && s.filePath == "" {
		return fmt.Errorf("must specify --rtr or --file")
	}

	// Register callback for UPDATE messages
	s.P.OnMsg(s.validate, s.Dir, msg.UPDATE)

	return nil
}

func (s *Rpki) Prepare() error {
	switch {
	case s.rtrAddr != "":
		go s.rtrRun()
	case s.filePath != "":
		go s.fileRun()
	default:
		panic("no RPKI source configured")
	}

	// Block until cache is ready or timeout
	select {
	case err := <-s.rtrUpdate:
		if err != nil {
			return fmt.Errorf("failed to init ROA cache: %w", err)
		}
		s.Info().Msg("ROA cache ready")
		return nil // unblock
	case <-time.After(s.readyTimeout):
		return fmt.Errorf("timeout waiting for ROA cache")
	case <-s.Ctx.Done():
		return s.Ctx.Err()
	}
}

func (s *Rpki) Stop() error {
	// TODO: stop RTR session?
	return nil
}
