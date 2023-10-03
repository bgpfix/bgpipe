package bgpipe

import (
	"context"
	"errors"

	"github.com/bgpfix/bgpfix/pipe"
)

// runStart starts Stage.Run in background iff needed.
// Cancels the main bgpipe context on error,
// or calls s.runStop otherwise (which respects b.wg_*).
// Manages s.enabled.
func (s *StageBase) runStart(ev *pipe.Event) (keep bool) {
	if s.started.Swap(true) || s.stopped.Load() {
		return // already started or stopped
	}

	// wrap Run
	s.Debug().Stringer("ev", ev).Msg("starting")
	s.enabled.Store(true)
	go func() {
		err := s.Stage.Run()
		s.enabled.Store(false)
		s.Trace().Stringer("ev", ev).Err(err).Msg("run finished")

		// no error or stopped due to --stop?
		if err == nil || errors.Is(err, ErrStageStopped) {
			s.runStop(nil)
		} else { // ...otherwise it's game over
			s.B.Cancel(s.Errorf("%w", err))
		}
	}()

	return
}

// runStop requests to stop Stage.Run
func (s *StageBase) runStop(ev *pipe.Event) (keep bool) {
	if s.stopped.Swap(true) {
		return // already stopped
	}

	s.Debug().Stringer("ev", ev).Msg("stopping")
	s.Cancel(ErrStageStopped)
	s.enabled.Store(false)
	s.wgAdd(-1)

	return
}

// Run is the default Stage implementation that just waits
// for the context and returns its cancel cause
func (s *StageBase) Run() error {
	<-s.Ctx.Done()
	return context.Cause(s.Ctx)
}
