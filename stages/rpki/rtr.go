package rpki

import (
	"time"

	rtrlib "github.com/bgp/stayrtr/lib"
	"github.com/bgpfix/bgpipe/pkg/util"
)

// rtrRun runs the RTR client with reconnection logic
func (s *Rpki) rtrRun() {
	k := s.K
	config := rtrlib.ClientConfiguration{
		ProtocolVersion: rtrlib.PROTOCOL_VERSION_1,
		Log:             &util.Stdlog{Logger: s.Logger},
	}

	// start the refresh goroutine
	go s.rtrRefresh(k.Duration("rtr-refresh"))

	for s.Ctx.Err() == nil {
		// NB: measure retry time vs. dial time, to protect from
		// retrying too fast if the server keeps dropping us
		retry := time.Now().Add(k.Duration("rtr-retry"))

		// connect
		conn, err := util.DialRetry(s.StageBase, nil, "tcp", s.rtr, k.Bool("rtr-tls"))
		if err != nil {
			s.Fatal().Err(err).Msg("could not connect to RTR server")
		}

		// make a new state
		rc := rtrlib.NewClientSession(config, s)
		s.rtr_mu.Lock()
		s.rtr_conn = conn
		s.rtr_client = rc
		s.rtr_mu.Unlock()

		// run RTR session (blocking until disconnected)
		err = rc.StartWithConn(conn)

		// clear the state
		s.rtr_mu.Lock()
		s.rtr_conn.Close()
		s.rtr_client = nil
		s.rtr_conn = nil
		s.rtr_mu.Unlock()

		// report, retry (with backoff to prevent rapid reconnect loops)
		if sleep := time.Until(retry); sleep > 0 {
			s.Warn().Err(err).Str("addr", s.rtr).Msgf("RTR connection failed, retrying in %s", sleep.Round(time.Second))
			time.Sleep(sleep)
		} else {
			s.Warn().Err(err).Str("addr", s.rtr).Msg("RTR connection failed, reconnecting")
		}
	}
}

// rtrSessionCheck checks if the session ID has changed
func (s *Rpki) rtrSessionCheck(rc *rtrlib.ClientSession, vs uint16) bool {
	if s.rtr_valid && s.rtr_sessid == vs {
		return true
	}

	s.Info().Uint16("old", s.rtr_sessid).Uint16("new", vs).Msg("RTR session changed")
	s.rtr_sessid = vs
	s.rtr_valid = false
	s.nextFlush()
	rc.SendResetQuery()

	return false
}

// rtrRefresh sends periodic Serial Query to check for updates
func (s *Rpki) rtrRefresh(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.rtr_mu.Lock()
			rs := s.rtr_client
			if rs != nil && s.rtr_valid {
				s.Debug().
					Uint16("session", s.rtr_sessid).
					Uint32("serial", s.rtr_serial).Msg("RTR periodic refresh")
				rs.SendSerialQuery(s.rtr_sessid, s.rtr_serial)
			}
			s.rtr_mu.Unlock()
		case <-s.Ctx.Done():
			return
		}
	}
}

// HandlePDU implements rtrlib.RTRClientSessionEventHandler
// It is called serially from the RTR client goroutine (no concurrency issues).
func (s *Rpki) HandlePDU(rc *rtrlib.ClientSession, pdu rtrlib.PDU) {
	switch p := pdu.(type) {
	case *rtrlib.PDUIPv4Prefix:
		s.nextRoa(p.Flags == rtrlib.FLAG_ADDED, p.Prefix, p.MaxLen, p.ASN)
	case *rtrlib.PDUIPv6Prefix:
		s.nextRoa(p.Flags == rtrlib.FLAG_ADDED, p.Prefix, p.MaxLen, p.ASN)

	case *rtrlib.PDUEndOfData:
		s.Debug().
			Uint16("session", p.SessionId).
			Uint32("serial", p.SerialNumber).Msg("RTR end of data")
		s.rtr_mu.Lock()
		defer s.rtr_mu.Unlock()

		if s.rtr_valid && s.rtr_serial == p.SerialNumber {
			return // no change
		}

		s.rtr_sessid = p.SessionId
		s.rtr_serial = p.SerialNumber
		s.rtr_valid = true
		s.nextApply()

	case *rtrlib.PDUCacheReset:
		s.Info().Msg("RTR cache reset requested")
		s.rtr_mu.Lock()
		defer s.rtr_mu.Unlock()

		s.rtr_valid = false
		s.nextFlush()
		rc.SendResetQuery()

	case *rtrlib.PDUCacheResponse:
		s.Debug().Uint16("session", p.SessionId).Msg("RTR cache response")
		s.rtr_mu.Lock()
		defer s.rtr_mu.Unlock()

		s.rtrSessionCheck(rc, p.SessionId)

	case *rtrlib.PDUSerialNotify:
		s.Debug().
			Uint16("session", p.SessionId).
			Uint32("serial", p.SerialNumber).Msg("RTR serial notify")
		s.rtr_mu.Lock()
		defer s.rtr_mu.Unlock()

		if !s.rtrSessionCheck(rc, p.SessionId) {
			return // session changed, reset already sent
		} else if p.SerialNumber != s.rtr_serial {
			rc.SendSerialQuery(s.rtr_sessid, s.rtr_serial)
		}

	case *rtrlib.PDUErrorReport:
		s.Warn().Uint16("code", p.ErrorCode).Str("text", p.ErrorMsg).Msg("RTR error")
		s.rtr_mu.Lock()
		defer s.rtr_mu.Unlock()

		s.rtr_valid = false
		s.nextFlush()
		rc.SendResetQuery()
	}
}

// ClientConnected implements rtrlib.RTRClientSessionEventHandler
func (s *Rpki) ClientConnected(rc *rtrlib.ClientSession) {
	s.rtr_mu.Lock()
	defer s.rtr_mu.Unlock()

	if s.rtr_valid { // on reconnect, try serial query first to get incremental update
		s.Info().Uint16("session", s.rtr_sessid).Uint32("serial", s.rtr_serial).Msg("RTR connected, requesting incremental update")
		rc.SendSerialQuery(s.rtr_sessid, s.rtr_serial)
	} else {
		s.Info().Msg("RTR connected, requesting full cache")
		s.nextFlush()
		rc.SendResetQuery()
	}
}

// ClientDisconnected implements rtrlib.RTRClientSessionEventHandler
func (s *Rpki) ClientDisconnected(rc *rtrlib.ClientSession) {
	s.Warn().Str("addr", s.rtr).Msg("RTR disconnected")
}
