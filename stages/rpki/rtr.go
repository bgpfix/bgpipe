package rpki

import (
	"crypto/tls"
	"net/netip"
	"slices"
	"time"

	rtrlib "github.com/bgp/stayrtr/lib"
)

// rtrRun runs the RTR client with reconnection logic
// FIXME: double-check all the intervals are respected (like refresh, retry, etc)
func (s *Rpki) rtrRun() {
	backoff := time.Second
	s.nextReset()

	k := s.K
	config := rtrlib.ClientConfiguration{
		ProtocolVersion: rtrlib.PROTOCOL_VERSION_1,
		RefreshInterval: uint32(k.Duration("rtr-refresh").Seconds()),
		RetryInterval:   uint32(k.Duration("rtr-retry").Seconds()),
		ExpireInterval:  uint32(k.Duration("rtr-expire").Seconds()),
		Log:             &Logger{s.Logger},
	}
	tlsConfig := &tls.Config{
		InsecureSkipVerify: k.Bool("rtr-insecure"),
	}

	for s.Ctx.Err() == nil {
		// connect RTR and block until disconnect
		// TODO: connection timeout? context management?
		start := time.Now()
		var err error
		s.rtrSession = rtrlib.NewClientSession(config, s)
		if s.rtrTLS {
			err = s.rtrSession.StartTLS(s.rtrAddr, tlsConfig)
		} else {
			err = s.rtrSession.StartPlain(s.rtrAddr)
		}

		// reset backoff if connected for over an hour
		if time.Since(start) > time.Hour {
			backoff = time.Second
		}

		// report, retry after backoff
		s.Err(err).Str("addr", s.rtrAddr).Msg("RTR connection ended, retrying...")
		select {
		case <-s.Ctx.Done():
			return
		case <-time.After(backoff):
			backoff = min(backoff*2, 5*time.Minute)
		}
	}
}

// HandlePDU implements rtrlib.RTRClientSessionEventHandler
// It is called serially from the RTR client goroutine (no concurrency issues).
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
		s.nextReset()
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
	s.nextReset()
	session.SendResetQuery()
}

// ClientDisconnected implements rtrlib.RTRClientSessionEventHandler
func (s *Rpki) ClientDisconnected(session *rtrlib.ClientSession) {
	s.Warn().Str("addr", s.rtrAddr).Msg("RTR disconnected")
}

// handlePrefix processes a single VRP from RTR
func (s *Rpki) handlePrefix(prefix netip.Prefix, maxLen uint8, asn uint32, flags uint8) {
	p := prefix.Masked()
	next := s.next4
	if p.Addr().Is6() {
		next = s.next6
	}

	// entry already exists?
	entry := ROAEntry{MaxLen: maxLen, ASN: asn}
	i := slices.Index(next[p], entry)

	if flags == rtrlib.FLAG_ADDED {
		if i < 0 {
			next[p] = append(next[p], entry)
		}
	} else {
		if i >= 0 {
			next[p] = slices.Delete(next[p], i, i+1)
		}
	}
}

// nextReset resets the pending VRP maps
func (s *Rpki) nextReset() {
	if roa4 := s.roa4.Load(); roa4 != nil && len(*roa4) > 0 {
		s.next4 = make(map[netip.Prefix][]ROAEntry, len(*roa4))
		for p, entries := range *roa4 {
			if len(entries) > 0 {
				s.next4[p] = slices.Clone(entries)
			}
		}
	} else {
		s.next4 = make(map[netip.Prefix][]ROAEntry)
	}

	if roa6 := s.roa6.Load(); roa6 != nil && len(*roa6) > 0 {
		s.next6 = make(map[netip.Prefix][]ROAEntry, len(*roa6))
		for p, entries := range *roa6 {
			if len(entries) > 0 {
				s.next6[p] = slices.Clone(entries)
			}
		}
	} else {
		s.next6 = make(map[netip.Prefix][]ROAEntry)
	}
}

// applyPendingChanges applies batched VRP changes to the cache
func (s *Rpki) applyPendingChanges() {
	// publish new maps
	s.roa4.Store(&s.next4)
	s.roa6.Store(&s.next6)
	s.Info().Int("v4", len(s.next4)).Int("v6", len(s.next6)).Msg("RTR cache updated")

	// signal update
	select {
	case s.rtrUpdate <- nil:
	default:
	}

	// request a new set of next4/next6
	s.nextReset()
}
