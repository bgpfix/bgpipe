/*
 * rpki: real-time RPKI validation of BGP UPDATE messages
 *
 * Maintains an in-memory ROA cache populated via RTR protocol (primary)
 * or JSON/CSV file monitoring (backup).
 *
 * License: MIT
 * Author: Pawel Foremski <pjf@foremski.pl>
 */

package stages

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	rtrlib "github.com/bgp/stayrtr/lib"
	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
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
	RPKI_TAG             // Tag message but pass through unchanged
)

// ROAEntry represents a single VRP (Validated ROA Payload)
type ROAEntry struct {
	MaxLen uint8
	ASN    uint32
}

type Rpki struct {
	*core.StageBase

	// Config
	invalidAction int
	strict        bool

	// ROA cache (atomic pointers, readers just Load() and read)
	roaV4 atomic.Pointer[map[netip.Prefix][]ROAEntry]
	roaV6 atomic.Pointer[map[netip.Prefix][]ROAEntry]

	// Source config
	rtrAddr      string
	rtrTLS       bool
	filePath     string
	filePoll     time.Duration
	readyTimeout time.Duration

	// File watcher state
	fileLastMod  time.Time
	fileLastHash [32]byte

	// RTR client state
	rtrSession   *rtrlib.ClientSession
	rtrMu        sync.Mutex                         // protects rtrPending*
	rtrPendingV4 map[netip.Prefix][]ROAEntry        // pending VRP additions
	rtrPendingV6 map[netip.Prefix][]ROAEntry        // pending VRP additions
	rtrRemoveV4  map[netip.Prefix]map[ROAEntry]bool // pending VRP removals
	rtrRemoveV6  map[netip.Prefix]map[ROAEntry]bool // pending VRP removals
	rtrReady     chan struct{}                      // closed when first cache received
}

func NewRpki(parent *core.StageBase) core.Stage {
	s := &Rpki{StageBase: parent}
	o := &s.Options
	f := o.Flags

	o.Descr = "RPKI validation of UPDATE messages"
	o.FilterIn = true
	o.Bidir = true

	// RTR Configuration
	f.String("rtr", "", "RTR server address (host:port)")
	f.Bool("rtr-tls", false, "use TLS for RTR connection")
	f.String("rtr-ssh", "", "RTR server for SSH (host:port)")
	f.String("rtr-ssh-user", "", "SSH username")
	f.String("rtr-ssh-key", "", "SSH private key file")

	// File Configuration
	f.String("file", "", "ROA file path (JSON/CSV), auto-reloaded on changes")
	f.Duration("file-poll", 30*time.Second, "file modification check interval")

	// Validation Behavior
	f.String("invalid", "withdraw", "action for INVALID prefixes: withdraw|drop|tag")
	f.Bool("strict", false, "treat NOT_FOUND same as INVALID")

	// Operational
	f.Duration("ready-timeout", 60*time.Second, "timeout waiting for ROA cache")

	return s
}

func (s *Rpki) Attach() error {
	k := s.K

	// Parse invalid action
	switch k.String("invalid") {
	case "withdraw":
		s.invalidAction = RPKI_WITHDRAW
	case "drop":
		s.invalidAction = RPKI_DROP
	case "tag":
		s.invalidAction = RPKI_TAG
	default:
		return fmt.Errorf("--invalid must be withdraw, drop, or tag")
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
	ready := make(chan struct{})
	go s.runSource(ready)

	// Block until cache is ready or timeout
	select {
	case <-ready:
		s.Info().Msg("ROA cache ready")
		return nil
	case <-time.After(s.readyTimeout):
		return fmt.Errorf("timeout waiting for ROA cache")
	case <-s.Ctx.Done():
		return s.Ctx.Err()
	}
}

func (s *Rpki) Run() error {
	// Main loop handled by runSource goroutine
	<-s.Ctx.Done()
	return nil
}

func (s *Rpki) Stop() error {
	return nil
}

// runSource manages the ROA cache source (file or RTR)
func (s *Rpki) runSource(ready chan struct{}) {
	var firstLoad bool

	// File source - load initially
	if s.filePath != "" {
		if err := s.loadFile(); err != nil {
			s.Error().Err(err).Msg("failed to load ROA file")
		} else {
			firstLoad = true
		}
	}

	// RTR source - runs in background
	if s.rtrAddr != "" {
		s.rtrReady = ready
		go s.runRTR()
		// RTR will signal ready when first cache is received
		// If file already loaded, signal ready now
		if firstLoad {
			close(ready)
			s.rtrReady = nil // prevent double close
		}
		// Wait for context to be done (RTR runs in its own goroutine)
		if s.filePath != "" {
			// Also poll file
			s.runFilePoll(nil, true)
		} else {
			<-s.Ctx.Done()
		}
		return
	}

	// Signal ready if we loaded anything (file only mode)
	if firstLoad {
		close(ready)
	}

	// Continue with file polling if configured
	if s.filePath != "" {
		s.runFilePoll(ready, firstLoad)
	}
}

// runFilePoll polls the file for changes
func (s *Rpki) runFilePoll(ready chan struct{}, alreadyReady bool) {
	ticker := time.NewTicker(s.filePoll)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			info, err := os.Stat(s.filePath)
			if err != nil {
				s.Warn().Err(err).Msg("failed to stat ROA file")
				continue
			}

			if info.ModTime().After(s.fileLastMod) {
				if err := s.loadFile(); err != nil {
					s.Warn().Err(err).Msg("failed to reload ROA file")
				} else if !alreadyReady {
					close(ready)
					alreadyReady = true
				}
			}

		case <-s.Ctx.Done():
			return
		}
	}
}

// loadFile loads ROA data from file
func (s *Rpki) loadFile() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	// Check if content actually changed
	hash := sha256.Sum256(data)
	if hash == s.fileLastHash {
		return nil
	}

	// Parse based on format
	v4 := make(map[netip.Prefix][]ROAEntry)
	v6 := make(map[netip.Prefix][]ROAEntry)

	if err := s.parseFile(data, v4, v6); err != nil {
		return err
	}

	// Atomic swap
	s.roaV4.Store(&v4)
	s.roaV6.Store(&v6)

	s.fileLastMod = time.Now()
	s.fileLastHash = hash

	s.Info().Int("v4", len(v4)).Int("v6", len(v6)).Msg("ROA file loaded")
	return nil
}

// parseFile parses ROA data from JSON or CSV
func (s *Rpki) parseFile(data []byte, v4, v6 map[netip.Prefix][]ROAEntry) error {
	// Try JSON first
	if len(data) > 0 && data[0] == '{' {
		return s.parseJSON(data, v4, v6)
	}

	// Fall back to CSV
	return s.parseCSV(data, v4, v6)
}

// parseJSON parses Routinator-style JSON
// Format: {"roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": "AS65001"}, ...]}
func (s *Rpki) parseJSON(data []byte, v4, v6 map[netip.Prefix][]ROAEntry) error {
	var doc struct {
		ROAs []struct {
			Prefix    string `json:"prefix"`
			MaxLength int    `json:"maxLength"`
			ASN       any    `json:"asn"` // can be string "AS65001" or int 65001
		} `json:"roas"`
	}

	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("JSON parse error: %w", err)
	}

	for _, roa := range doc.ROAs {
		prefix, err := netip.ParsePrefix(roa.Prefix)
		if err != nil {
			s.Warn().Str("prefix", roa.Prefix).Msg("invalid prefix, skipping")
			continue
		}
		prefix = prefix.Masked()

		// Parse ASN (handle both "AS65001" and 65001)
		var asn uint32
		switch v := roa.ASN.(type) {
		case string:
			v = strings.TrimPrefix(v, "AS")
			v = strings.TrimPrefix(v, "as")
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				s.Warn().Str("asn", fmt.Sprint(roa.ASN)).Msg("invalid ASN, skipping")
				continue
			}
			asn = uint32(n)
		case float64:
			asn = uint32(v)
		default:
			s.Warn().Str("asn", fmt.Sprint(roa.ASN)).Msg("invalid ASN type, skipping")
			continue
		}

		entry := ROAEntry{
			MaxLen: uint8(roa.MaxLength),
			ASN:    asn,
		}

		if prefix.Addr().Is4() {
			v4[prefix] = append(v4[prefix], entry)
		} else {
			v6[prefix] = append(v6[prefix], entry)
		}
	}

	return nil
}

// parseCSV parses CSV format: prefix,maxLength,asn
func (s *Rpki) parseCSV(data []byte, v4, v6 map[netip.Prefix][]ROAEntry) error {
	lines := strings.Split(string(data), "\n")

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}

		// Skip header
		if i == 0 && strings.Contains(strings.ToLower(line), "prefix") {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			s.Warn().Int("line", i+1).Msg("invalid CSV line, skipping")
			continue
		}

		prefix, err := netip.ParsePrefix(strings.TrimSpace(parts[0]))
		if err != nil {
			s.Warn().Int("line", i+1).Str("prefix", parts[0]).Msg("invalid prefix, skipping")
			continue
		}
		prefix = prefix.Masked()

		maxLen, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			s.Warn().Int("line", i+1).Msg("invalid maxLength, skipping")
			continue
		}

		asnStr := strings.TrimSpace(parts[2])
		asnStr = strings.TrimPrefix(asnStr, "AS")
		asnStr = strings.TrimPrefix(asnStr, "as")
		asn, err := strconv.ParseUint(asnStr, 10, 32)
		if err != nil {
			s.Warn().Int("line", i+1).Msg("invalid ASN, skipping")
			continue
		}

		entry := ROAEntry{
			MaxLen: uint8(maxLen),
			ASN:    uint32(asn),
		}

		if prefix.Addr().Is4() {
			v4[prefix] = append(v4[prefix], entry)
		} else {
			v6[prefix] = append(v6[prefix], entry)
		}
	}

	return nil
}

// validatePrefix performs RPKI validation for a single prefix
func (s *Rpki) validatePrefix(p netip.Prefix, origin uint32) int {
	// get ROA cache
	var roas map[netip.Prefix][]ROAEntry
	var minLen int
	if p.Addr().Is4() {
		minLen = minROALenV4
		if v := s.roaV4.Load(); v != nil {
			roas = *v
		}
	} else {
		minLen = minROALenV6
		if v := s.roaV6.Load(); v != nil {
			roas = *v
		}
	}
	if len(roas) == 0 {
		return RPKI_NOT_FOUND
	}

	// Check covering prefixes from most-specific to least-specific
	var found bool
	addr, bits, try := p.Addr(), uint8(p.Bits()), p.Bits()
	for {
		for _, e := range roas[p] {
			if origin == e.ASN && bits <= e.MaxLen {
				return RPKI_VALID
			} else {
				found = true
			}
		}

		// retry with less specific prefix?
		if try > minLen {
			try--
			p, _ = addr.Prefix(try)
		} else {
			break
		}
	}

	if found {
		return RPKI_INVALID
	} else if s.strict {
		return RPKI_INVALID
	} else {
		return RPKI_NOT_FOUND
	}
}

// validate is the callback for UPDATE messages
func (s *Rpki) validate(m *msg.Msg) (keep bool) {
	u := &m.Update
	mx := pipe.UseContext(m)
	keep = true

	// Get origin AS from AS_PATH
	// TODO: AS_SET paths make ROA-covered prefixes INVALID
	origin := u.AsPath().Origin()
	if origin == 0 {
		return true // empty/AS_SET origin, pass through
	}

	// check_delete checks a prefix and decides whether to delete it
	var invalid []nlri.NLRI
	check_delete := func(p nlri.NLRI) bool {
		if !keep {
			return false // already decided to drop m
		} else if s.validatePrefix(p.Prefix, origin) != RPKI_INVALID {
			return false // not bad enough, let's keep it
		}

		// drop the whole message?
		if s.invalidAction == RPKI_DROP {
			keep = false
			return false
		}

		// mark as invalid
		invalid = append(invalid, p)
		tags := mx.UseTags()
		tags["rpki/status"] = "INVALID"
		tags["rpki/"+p.String()] = "INVALID"

		return s.invalidAction == RPKI_WITHDRAW
	}

	// check IPv4 reachable prefixes
	u.Reach = slices.DeleteFunc(u.Reach, check_delete)
	if !keep {
		return false
	}

	// check MP reachable prefixes
	mpp := u.ReachMP().Prefixes()
	if mpp != nil && mpp.Len() > 0 {
		mpp.Prefixes = slices.DeleteFunc(mpp.Prefixes, check_delete)
		if !keep {
			return false
		}
	}

	// check the result
	if len(invalid) > 0 {
		m.Edit()

		// need to write invalid prefixes to unreach?
		if s.invalidAction == RPKI_WITHDRAW {
			u.AddUnreach(invalid...)
			if mpp != nil && mpp.Len() == 0 {
				u.Attrs.Drop(attrs.ATTR_MP_REACH)
			}
		}
	}

	return true
}

// ============================================================================
// RTR Client Implementation
// ============================================================================

// runRTR runs the RTR client with reconnection logic
func (s *Rpki) runRTR() {
	backoff := time.Second

	for {
		select {
		case <-s.Ctx.Done():
			return
		default:
		}

		// Create RTR session
		config := rtrlib.ClientConfiguration{
			ProtocolVersion: rtrlib.PROTOCOL_VERSION_1,
			RefreshInterval: 3600,
			RetryInterval:   600,
			ExpireInterval:  7200,
		}
		s.rtrSession = rtrlib.NewClientSession(config, s)

		// Connect
		var err error
		if s.rtrTLS {
			tlsConfig := &tls.Config{
				InsecureSkipVerify: false,
			}
			err = s.rtrSession.StartTLS(s.rtrAddr, tlsConfig)
		} else {
			err = s.rtrSession.StartPlain(s.rtrAddr)
		}

		if err != nil {
			s.Error().Err(err).Str("addr", s.rtrAddr).Msg("RTR connection failed")
			select {
			case <-s.Ctx.Done():
				return
			case <-time.After(backoff):
				backoff = min(backoff*2, 5*time.Minute)
				continue
			}
		}

		// Reset backoff on successful connection
		backoff = time.Second

		// Wait for disconnect or context done
		select {
		case <-s.Ctx.Done():
			return
		}
	}
}

// HandlePDU implements rtrlib.RTRClientSessionEventHandler
func (s *Rpki) HandlePDU(session *rtrlib.ClientSession, pdu rtrlib.PDU) {
	switch p := pdu.(type) {
	case *rtrlib.PDUIPv4Prefix:
		s.handlePrefix(p.Prefix, p.MaxLen, p.ASN, p.Flags)
	case *rtrlib.PDUIPv6Prefix:
		s.handlePrefix(p.Prefix, p.MaxLen, p.ASN, p.Flags)
	case *rtrlib.PDUEndOfData:
		s.applyPendingChanges()
		s.Info().Uint32("serial", p.SerialNumber).Msg("RTR end of data")
	case *rtrlib.PDUCacheReset:
		s.Info().Msg("RTR cache reset requested")
		// Clear cache and request full reload
		s.clearPending()
		session.SendResetQuery()
	case *rtrlib.PDUCacheResponse:
		s.Debug().Uint16("session", p.SessionId).Msg("RTR cache response")
	case *rtrlib.PDUSerialNotify:
		s.Debug().Uint32("serial", p.SerialNumber).Msg("RTR serial notify")
	case *rtrlib.PDUErrorReport:
		s.Warn().Uint16("code", p.ErrorCode).Str("text", p.ErrorMsg).Msg("RTR error")
	}
}

// ClientConnected implements rtrlib.RTRClientSessionEventHandler
func (s *Rpki) ClientConnected(session *rtrlib.ClientSession) {
	s.Info().Str("addr", s.rtrAddr).Msg("RTR connected")
	// Initialize pending maps
	s.clearPending()
	// Request full cache
	session.SendResetQuery()
}

// ClientDisconnected implements rtrlib.RTRClientSessionEventHandler
func (s *Rpki) ClientDisconnected(session *rtrlib.ClientSession) {
	s.Warn().Str("addr", s.rtrAddr).Msg("RTR disconnected")
}

// handlePrefix processes a single VRP from RTR
func (s *Rpki) handlePrefix(prefix netip.Prefix, maxLen uint8, asn uint32, flags uint8) {
	prefix = prefix.Masked()
	entry := ROAEntry{MaxLen: maxLen, ASN: asn}

	s.rtrMu.Lock()
	defer s.rtrMu.Unlock()

	if flags == rtrlib.FLAG_ADDED {
		// Add to pending
		if prefix.Addr().Is4() {
			s.rtrPendingV4[prefix] = append(s.rtrPendingV4[prefix], entry)
		} else {
			s.rtrPendingV6[prefix] = append(s.rtrPendingV6[prefix], entry)
		}
	} else {
		// Mark for removal
		if prefix.Addr().Is4() {
			if s.rtrRemoveV4[prefix] == nil {
				s.rtrRemoveV4[prefix] = make(map[ROAEntry]bool)
			}
			s.rtrRemoveV4[prefix][entry] = true
		} else {
			if s.rtrRemoveV6[prefix] == nil {
				s.rtrRemoveV6[prefix] = make(map[ROAEntry]bool)
			}
			s.rtrRemoveV6[prefix][entry] = true
		}
	}
}

// clearPending resets the pending VRP maps
func (s *Rpki) clearPending() {
	s.rtrMu.Lock()
	defer s.rtrMu.Unlock()
	s.rtrPendingV4 = make(map[netip.Prefix][]ROAEntry)
	s.rtrPendingV6 = make(map[netip.Prefix][]ROAEntry)
	s.rtrRemoveV4 = make(map[netip.Prefix]map[ROAEntry]bool)
	s.rtrRemoveV6 = make(map[netip.Prefix]map[ROAEntry]bool)
}

// applyPendingChanges applies batched VRP changes to the cache
func (s *Rpki) applyPendingChanges() {
	s.rtrMu.Lock()
	pendingV4 := s.rtrPendingV4
	pendingV6 := s.rtrPendingV6
	removeV4 := s.rtrRemoveV4
	removeV6 := s.rtrRemoveV6
	s.rtrPendingV4 = make(map[netip.Prefix][]ROAEntry)
	s.rtrPendingV6 = make(map[netip.Prefix][]ROAEntry)
	s.rtrRemoveV4 = make(map[netip.Prefix]map[ROAEntry]bool)
	s.rtrRemoveV6 = make(map[netip.Prefix]map[ROAEntry]bool)
	s.rtrMu.Unlock()

	// Copy current cache
	oldV4 := s.roaV4.Load()
	oldV6 := s.roaV6.Load()

	newV4 := make(map[netip.Prefix][]ROAEntry)
	newV6 := make(map[netip.Prefix][]ROAEntry)

	// Copy existing entries (excluding removed ones)
	if oldV4 != nil {
		for prefix, entries := range *oldV4 {
			toRemove := removeV4[prefix]
			for _, e := range entries {
				if toRemove == nil || !toRemove[e] {
					newV4[prefix] = append(newV4[prefix], e)
				}
			}
		}
	}
	if oldV6 != nil {
		for prefix, entries := range *oldV6 {
			toRemove := removeV6[prefix]
			for _, e := range entries {
				if toRemove == nil || !toRemove[e] {
					newV6[prefix] = append(newV6[prefix], e)
				}
			}
		}
	}

	// Add new entries
	for prefix, entries := range pendingV4 {
		newV4[prefix] = append(newV4[prefix], entries...)
	}
	for prefix, entries := range pendingV6 {
		newV6[prefix] = append(newV6[prefix], entries...)
	}

	// Atomic swap
	s.roaV4.Store(&newV4)
	s.roaV6.Store(&newV6)

	s.Info().Int("v4", len(newV4)).Int("v6", len(newV6)).Msg("RTR cache updated")

	// Signal ready if this is the first load
	if s.rtrReady != nil {
		close(s.rtrReady)
		s.rtrReady = nil
	}
}
