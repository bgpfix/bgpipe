package core

import (
	"context"
	"errors"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
)

// runStart starts Stage.Run in background iff needed.
// Cancels the main bgpipe context on error,
// or calls s.runStop otherwise (which respects b.wg_*).
// Controls the s.enabled switch.
func (s *StageBase) runStart(ev *pipe.Event) bool {
	if s.started.Swap(true) || s.stopped.Load() {
		return false // already started or stopped
	} else {
		s.Debug().Stringer("ev", ev).Msg("starting")
	}

	// check if err and s.Ctx ok; cancel global ctx otherwise
	check_fatal := func(err error) bool {
		if err == nil {
			err = context.Cause(s.Ctx)
		}
		if err == nil || err == context.Canceled || errors.Is(err, ErrStageStopped) {
			return false
		} else {
			s.B.Cancel(s.Errorf("%w", err)) // game over
			return true
		}
	}

	// run Prepare, make sure to get the error back
	s.Trace().Msg("Prepare()")
	s.Event("PREPARE")
	err := s.Stage.Prepare()
	s.Trace().Err(err).Msg("Prepare() done")
	if check_fatal(err) {
		return false
	} else {
		s.Event("READY")
	}

	// enable callbacks and handlers
	s.running.Store(true)

	// start Stage.Run in background
	go func() {
		// wait for all stages started in this event to finish Prepare()
		ev.Wait()

		// block on Run if context still valid
		err := context.Cause(s.Ctx)
		if err == nil {
			s.Trace().Msg("Run() starting")
			s.Event("START")
			err = s.Stage.Run()
			s.Trace().Err(err).Msg("Run() returned")
		}

		// disable callbacks and handlers
		s.running.Store(false)
		close(s.done)

		// fatal error?
		if check_fatal(err) {
			return // the whole process will exit
		} else {
			s.runStop(nil) // cleanup
		}
	}()

	return false
}

// runStop requests to stop Stage.Run; ev may be nil
func (s *StageBase) runStop(ev *pipe.Event) bool {
	if s == nil || s.stopped.Swap(true) {
		return false // already stopped, or not started yet
	} else {
		s.Debug().Stringer("ev", ev).Msg("stopping")
	}

	// stage Run() still did not return?
	if s.running.Load() {
		// request to stop
		go func() {
			err := s.Stage.Stop()
			if err != nil {
				s.Err(err).Msg("stage Stop() error")
			}
		}()

		// give it time to exit cleanly?
		if t := s.Options.StopTimeout; t >= 0 {
			if t == 0 {
				t = 3 * time.Second
			}
			select {
			case <-s.done:
			case <-time.After(t):
				s.Warn().Msg("stop timeout, forcing cancel")
				s.Cancel(ErrStageStopped)
			}
		} else {
			s.Cancel(ErrStageStopped)
		}
	}

	// close all inputs and wait for them to finish processing
	for _, in := range s.inputs {
		in.Close()
	}
	for _, in := range s.inputs {
		in.Wait()
	}

	// pull the plug
	s.Cancel(ErrStageStopped)
	s.running.Store(false)
	s.wgAdd(-1)
	s.Event("STOP")
	s.Debug().Msg("stopped")
	return false
}
