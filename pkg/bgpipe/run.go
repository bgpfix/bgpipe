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

	// check if err and s.Ctx ok; cancel global ctx otherwise
	iserr := func(err error) bool {
		if err == nil {
			err = context.Cause(s.Ctx)
			if err == context.Canceled {
				err = nil
			}
		}
		if err == nil || errors.Is(err, ErrStageStopped) {
			return false
		}

		// game over
		s.B.Cancel(s.Errorf("%w", err))
		return true
	}

	// run Prepare, make sure to get the error back
	s.Trace().Msg("Prepare start")
	err := s.Stage.Prepare()
	s.Trace().Err(err).Msg("Prepare done")
	if iserr(err) {
		return
	}

	// enable callbacks and handlers
	s.running.Store(true)

	// start Stage.Run in background
	go func() {
		// catch stage panics
		defer func() {
			if r := recover(); r != nil {
				s.B.Cancel(s.Errorf("panic: %v", r)) // game over
			}
		}()

		// block on Run if context still valid
		err := context.Cause(s.Ctx)
		if err == nil {
			s.Trace().Msg("Run start")
			s.Event("READY", nil)
			err = s.Stage.Run()
			s.Trace().Err(err).Msg("Run done")
			s.Event("DONE", nil)
		}

		// disable callbacks and handlers
		s.running.Store(false)

		// exited cleanly? run ordinary stop
		if !iserr(err) {
			s.runStop(nil)
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
	s.running.Store(false)
	s.wgAdd(-1)

	return
}
