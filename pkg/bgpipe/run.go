package bgpipe

import (
	"context"
	"errors"

	"github.com/bgpfix/bgpfix/pipe"
)

// run wraps Stage.Run.
// Cancels the main bgpipe context on error.
// Respects b.wg_* waitgroups.
func (s *StageBase) run(ev string) {
	if s.started.Swap(true) || s.stopped.Load() {
		return // already started or stopped
	}

	// wrap Run
	s.Debug().Str("ev", ev).Msg("starting")
	s.enabled.Store(true)
	err := s.prepare()
	if err == nil {
		err = s.Stage.Run()
	}
	s.enabled.Store(false)
	s.Trace().Str("ev", ev).Err(err).Msg("run returned")

	// no error or stopped due to --off?
	if err == nil || errors.Is(err, ErrStageStopped) {
		s.runStop("finished")
	} else { // ...otherwise it's game over
		s.B.Cancel(s.Errorf("%w", err))
	}
}

// runStop requests to stop Stage.Run
func (s *StageBase) runStop(ev string) {
	if s.stopped.Swap(true) {
		return // already stopped
	}

	s.Debug().Str("ev", ev).Msg("stopping")
	s.Cancel(ErrStageStopped)
	s.enabled.Store(false)
	s.WgAdd(-1)
}

// runOn starts the stage in reaction to a pipe event
func (s *StageBase) runOn(ev *pipe.Event) (keep_event bool) {
	go s.run(ev.Type)
	return false
}

// runOff stops the stage in reaction to a pipe event
func (s *StageBase) runOff(ev *pipe.Event) (keep_event bool) {
	go s.runStop(ev.Type)
	return false
}

// Run is the default Stage implementation that just waits
// for the context and returns its cancel cause
func (s *StageBase) Run() error {
	<-s.Ctx.Done()
	return context.Cause(s.Ctx)
}

// prepare wraps Stage.Prepare
func (s *StageBase) prepare() error {
	if s.prepared.Swap(true) {
		return nil // already prepared
	}
	return s.Stage.Prepare()
}

// Prepare is the default Stage implementation that does nothing
func (s *StageBase) Prepare() error {
	return nil
}
