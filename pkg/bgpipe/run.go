package bgpipe

import (
	"context"
	"errors"

	"github.com/bgpfix/bgpfix/pipe"
)

// runStart starts Stage.Run in background iff needed.
// Cancels the main bgpipe context on error,
// or calls s.runStop otherwise (which respects b.wg_*).
// Controls the s.enabled switch.
func (s *StageBase) runStart(ev *pipe.Event) (keep bool) {
	if s.started.Swap(true) || s.stopped.Load() {
		return // already started or stopped
	} else {
		s.Debug().Stringer("ev", ev).Msg("starting")
	}

	// start Stage.Run in background
	go func() {
		// catch stage panics
		defer func() {
			if r := recover(); r != nil {
				s.B.Cancel(s.Errorf("panic: %v", r)) // game over
			}
		}()

		// run Prepare, make sure to get the error back
		s.Trace().Msg("Prepare start")
		err := s.Stage.Prepare()
		s.Trace().Err(err).Msg("Prepare done")

		// successful? enable callbacks/handlers and block on Run if context still valid
		if err == nil {
			s.enabled.Store(true)
			if err = context.Cause(s.Ctx); err == nil {
				s.Event("READY", nil)
				s.Trace().Msg("Run start")
				err = s.Stage.Run()
				s.Trace().Err(err).Msg("Run done")
			}
			s.enabled.Store(false)
		}

		// handle the error
		if err == nil || errors.Is(err, ErrStageStopped) {
			s.runStop(nil) // ordinary stop
		} else {
			s.B.Cancel(s.Errorf("%w", err)) // game over
		}
	}()

	return
}

// runStop requests to stop Stage.Run
func (s *StageBase) runStop(ev *pipe.Event) (keep bool) {
	if s.stopped.Swap(true) {
		return // already stopped
	} else {
		s.Debug().Stringer("ev", ev).Msg("stopping")
	}

	s.Cancel(ErrStageStopped)
	s.enabled.Store(false)
	s.wgAdd(-1)

	return
}
