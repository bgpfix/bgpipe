package bgpipe

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
func (s *StageBase) runStart(ev *pipe.Event) (keep bool) {
	if s.started.Swap(true) || s.stopped.Load() {
		return // already started or stopped
	} else {
		s.Debug().Stringer("ev", ev).Msg("starting")
	}

	// check if err and s.Ctx ok; cancel global ctx otherwise
	check_fatal := func(err error) bool {
		if err == nil {
			err = context.Cause(s.Ctx)
			if err == context.Canceled {
				err = nil
			}
		}
		if err == nil || errors.Is(err, ErrStageStopped) {
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
		return
	} else {
		s.Event("READY")
	}

	// enable callbacks and handlers
	s.running.Store(true)

	// start Stage.Run in background
	go func() {
		// wait for all stages started in this event to finish Prepare()
		ev.Wait()

		// catch stage panics
		defer func() {
			if r := recover(); r != nil {
				s.B.Cancel(s.Errorf("panic: %v", r)) // game over
			}
		}()

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

	return false // unregister
}

// runStop requests to stop Stage.Run
func (s *StageBase) runStop(ev *pipe.Event) (keep bool) {
	if s.stopped.Swap(true) {
		return // already stopped, or not started yet
	} else {
		s.Debug().Stringer("ev", ev).Msg("stopping")
	}

	err := ErrStageStopped

	// still running?
	if s.running.Load() {
		// request to stop
		err_stop := s.Stage.Stop()
		if err_stop != nil {
			err = err_stop
		}

		// give it 1s to exit cleanly
		select {
		case <-s.done:
		case <-time.After(time.Second):
		}
	}

	// close all inputs and wait for them to finish processing
	for _, in := range s.procs {
		in.Close()
	}
	for _, in := range s.procs {
		in.Wait()
	}

	// pull the plug
	s.Cancel(err)
	s.running.Store(false)
	s.wgAdd(-1)
	s.Event("STOP")

	return false // unregister
}
