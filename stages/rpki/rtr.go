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
		conn, err := util.DialRetry(s.StageBase, nil, "tcp", s.rtr)
		if err != nil {
			s.Fatal().Err(err).Msg("could not connect to RTR server")
		}

		// make a new state
		rc := rtrlib.NewClientSession(config, s)
		s.rtr_mu.Lock()
		s.rtr_conn = conn
		s.rtr_client = rc
		s.rtr_sessid = 0
		s.rtr_valid = false
		s.nextFlush()
		s.rtr_mu.Unlock()

		// run RTR session (blocks until disconnected)
		err = rc.StartWithConn(conn)

		// clear the state
		s.rtr_mu.Lock()
		s.rtr_conn.Close()
		s.rtr_client = nil
		s.rtr_conn = nil
		s.rtr_sessid = 0
		s.rtr_valid = false
		s.rtr_mu.Unlock()

		// report, retry
		if sleep := time.Until(retry); sleep > time.Second {
			s.Warn().Err(err).Str("addr", s.rtr).Msgf("RTR connection failed, retrying in %s", sleep.Round(time.Second))
			select {
			case <-time.After(sleep):
			case <-s.Ctx.Done():
			}
		} else {
			s.Warn().Err(err).Str("addr", s.rtr).Msg("RTR connection failed, retrying now")
		}
	}
}

// rtrSessionCheck checks if the session ID has changed
func (s *Rpki) rtrSessionCheck(rc *rtrlib.ClientSession, id uint16) bool {
	if s.rtr_valid && s.rtr_sessid == id {
		return true
	}

	if !s.rtr_valid && s.rtr_sessid == 0 {
		s.Info().Uint16("id", id).Msg("RTR new session")
		return true
	}

	s.Info().Uint16("old", s.rtr_sessid).Uint16("new", id).Msg("RTR session changed")
	s.rtr_sessid = id
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
			valid := s.rtr_valid
			sessid := s.rtr_sessid
			serial := s.rtr_serial
			s.rtr_mu.Unlock()

			if rs != nil && valid {
				s.Debug().
					Uint16("session", sessid).
					Uint32("serial", serial).Msg("RTR periodic refresh")
				rs.SendSerialQuery(sessid, serial)
			}
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

		// code 2 = "No Data Available" (eg. server still initializing);
		// do not retry immediately — wait for the periodic refresh instead
		if p.ErrorCode != rtrlib.PDU_ERROR_NODATA {
			rc.SendResetQuery()
		}
	}
}

// ClientConnected implements rtrlib.RTRClientSessionEventHandler
func (s *Rpki) ClientConnected(rc *rtrlib.ClientSession) {
	s.Debug().Msg("RTR connected, requesting full cache")
	rc.SendResetQuery()
}

// ClientDisconnected implements rtrlib.RTRClientSessionEventHandler
func (s *Rpki) ClientDisconnected(rc *rtrlib.ClientSession) {
	s.Debug().Msg("RTR disconnected")
}
