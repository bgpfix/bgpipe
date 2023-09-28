package bgpipe

import (
	"context"
	"errors"
)

// run wraps Stage.Run.
// Cancels the main bgpipe context on error.
// Respects b.wg_* waitgroups.
func (s *StageBase) run() error {
	if !s.started.CompareAndSwap(false, true) {
		return nil // already running
	} else if s.Ctx.Err() != nil {
		return nil // already stopped
	}

	// run the stage
	s.enabled.Store(true)
	err := s.Stage.Run()
	s.Debug().Msg("stopped")
	s.enabled.Store(false)

	// stopped due to stage disabled? ignore
	if errors.Is(err, ErrStageStopped) {
		err = nil
	}

	// cancel context
	if err != nil {
		s.B.Cancel(s.Errorf("%w", err))
	} else {
		s.Cancel(ErrStageStopped)
	}

	if s.isLReader() {
		s.B.wg_lread.Done()
	}
	if s.isLWriter() {
		s.B.wg_lwrite.Done()
	}
	if s.isRReader() {
		s.B.wg_rread.Done()
	}
	if s.isRWriter() {
		s.B.wg_rwrite.Done()
	}

	return err
}

// Run is the default Stage implementation that just waits
// for the context and returns its cancel cause
func (s *StageBase) Run() error {
	<-s.Ctx.Done()
	return context.Cause(s.Ctx)
}
