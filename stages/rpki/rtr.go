package rpki

import (
	"net/netip"
	"time"

	"github.com/bgpfix/bgpfix/rtr"
	"github.com/bgpfix/bgpipe/pkg/util"
)

// rtrRun manages the RTR client connection loop with reconnection.
func (s *Rpki) rtrRun() {
	k := s.K

	client := rtr.NewClient(&rtr.Options{
		Logger:  &s.Logger,
		Version: rtr.VersionAuto,

		OnROA: func(add bool, prefix netip.Prefix, maxLen uint8, asn uint32) {
			s.nextVRP(add, prefix, maxLen, asn)
		},

		OnASPA: func(add bool, cas uint32, providers []uint32) {
			s.nextASPA(add, cas, providers)
		},

		OnEndOfData: func(sessid uint16, serial uint32) {
			s.nextApply()
			s.rtr_mu.Lock()
			s.rtr_valid = true
			s.rtr_mu.Unlock()
		},

		OnCacheReset: func() {
			s.nextFlush()
			s.rtr_mu.Lock()
			s.rtr_valid = false
			s.rtr_mu.Unlock()
		},

		OnError: func(code uint16, text string) {
			if code != rtr.ErrNoData {
				s.Warn().Uint16("code", code).Str("text", text).Msg("RTR error")
			} else {
				s.Debug().Msg("RTR no data available yet")
			}
		},
	})

	go s.rtrRefresh(client, k.Duration("rtr-refresh"))

	for s.Ctx.Err() == nil {
		retry := time.Now().Add(k.Duration("rtr-retry"))

		conn, err := util.DialRetry(s.StageBase, nil, "tcp", s.rtr)
		if err != nil {
			s.Fatal().Err(err).Msg("could not connect to RTR server")
		}

		s.rtr_mu.Lock()
		s.rtr_conn = conn
		s.rtr_valid = false
		s.rtr_mu.Unlock()

		s.nextFlush()
		err = client.Run(s.Ctx, conn) // NB: Run always closes conn

		s.rtr_mu.Lock()
		s.rtr_conn = nil
		s.rtr_valid = false
		s.rtr_mu.Unlock()

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

// rtrRefresh sends periodic Serial Query to check for incremental updates.
func (s *Rpki) rtrRefresh(client *rtr.Client, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.rtr_mu.Lock()
			valid := s.rtr_valid
			s.rtr_mu.Unlock()
			if valid {
				s.Debug().Msg("RTR periodic refresh")
				client.SendSerial()
			}
		case <-s.Ctx.Done():
			return
		}
	}
}
