package rpki

import (
	"net/netip"

	rtrlib "github.com/bgp/stayrtr/lib"
	"github.com/bgpfix/bgpipe/pkg/util"
)

// rtrRun runs the RTR client with reconnection logic
// FIXME: double-check all the intervals are respected (like refresh, retry, etc)
func (s *Rpki) rtrRun() {
	k := s.K
	config := rtrlib.ClientConfiguration{
		ProtocolVersion: rtrlib.PROTOCOL_VERSION_1,
		RefreshInterval: uint32(k.Duration("rtr-refresh").Seconds()),
		RetryInterval:   uint32(k.Duration("rtr-retry").Seconds()),
		ExpireInterval:  uint32(k.Duration("rtr-expire").Seconds()),
		Log:             &util.Stdlog{Logger: s.Logger},
	}

	for s.Ctx.Err() == nil {
		// connect
		conn, err := util.DialRetry(s.StageBase, nil, "tcp", s.rtr, s.tls)
		if err != nil {
			s.Fatal().Err(err).Msg("could not connect to RTR server")
		} else {
			s.rtrConn.Store(&conn)
		}

		// run RTR session
		s.rtrSession = rtrlib.NewClientSession(config, s)
		err = s.rtrSession.StartWithConn(conn)

		// report, retry
		s.Err(err).Str("addr", s.rtr).Msg("RTR connection done, reconnecting...")
	}
}

// HandlePDU implements rtrlib.RTRClientSessionEventHandler
// It is called serially from the RTR client goroutine (no concurrency issues).
func (s *Rpki) HandlePDU(session *rtrlib.ClientSession, pdu rtrlib.PDU) {
	switch p := pdu.(type) {
	case *rtrlib.PDUIPv4Prefix:
		s.rtrHandle(p.Prefix, p.MaxLen, p.ASN, p.Flags)
	case *rtrlib.PDUIPv6Prefix:
		s.rtrHandle(p.Prefix, p.MaxLen, p.ASN, p.Flags)

	case *rtrlib.PDUEndOfData:
		s.Info().Uint32("serial", p.SerialNumber).Msg("RTR end of data")
		s.nextApply()

	case *rtrlib.PDUCacheReset:
		s.Info().Msg("RTR cache reset requested")
		s.nextFlush()
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
	s.Info().Str("addr", s.rtr).Msg("RTR connected")
	s.nextFlush()
	session.SendResetQuery()
}

// ClientDisconnected implements rtrlib.RTRClientSessionEventHandler
func (s *Rpki) ClientDisconnected(session *rtrlib.ClientSession) {
	s.Warn().Str("addr", s.rtr).Msg("RTR disconnected")
}

// rtrHandle processes a single VRP from RTR
func (s *Rpki) rtrHandle(prefix netip.Prefix, maxLen uint8, asn uint32, flags uint8) {
	if flags == rtrlib.FLAG_ADDED {
		s.nextAdd(prefix, maxLen, asn)
	} else {
		s.nextDel(prefix, maxLen, asn)
	}
}
