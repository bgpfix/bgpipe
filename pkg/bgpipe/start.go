package bgpipe

import (
	"errors"
)

// pipeStart wraps Stage.Start.
// Cancels the main bgpipe context on error.
// Respects b.wg_* waitgroups.
func (s *StageBase) pipeStart() error {
	if !s.started.CompareAndSwap(false, true) {
		return nil // already running
	} else if s.Ctx.Err() != nil {
		return nil // already stopped
	}

	// run the stage
	s.Debug().Msg("starting")
	s.enabled.Store(true)
	err := s.Stage.Start()
	s.enabled.Store(false)

	// stopped due to stage disabled? ignore
	if errors.Is(err, ErrStageStopped) {
		err = nil
	}

	// force context cleanup anyway
	s.Cancel(ErrStageStopped)

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

	if err != nil {
		s.B.Cancel(s.Errorf("%w", err))
	}

	return err
}
