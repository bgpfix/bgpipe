package core

import (
	"context"
	"errors"
	"slices"
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
	if s.Options.StopUpstream {
		go s.stopUpstream()
	}

	// start Stage.Run in background
	go func() {
		// wait for all stages started in this event to finish Prepare()
		ev.Wait()

		// block on Run if context still valid
		err := context.Cause(s.Ctx)
		if err == ErrPipeFinished {
			// NB: normal pipe completion may beat a late Run() start; still run it,
			// so that write-side stages (eg. --stdout) drain their buffered output
			// before runStop() lets the process exit
			err = nil
		}
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

// stopUpstream stops s when all producers before it, in its direction, are done.
// NB: a stage is done after its inputs are drained, so s saw all their messages
func (s *StageBase) stopUpstream() {
	for _, st := range append(slices.Clone(s.B.Stages), s.B.Autos...) {
		if st == nil || st == s || !st.Options.IsProducer {
			continue
		} else if (s.IsRight && st.IsRight && st.Index < s.Index) || (s.IsLeft && st.IsLeft && st.Index > s.Index) {
			select {
			case <-st.Ctx.Done():
			case <-s.Ctx.Done():
				return
			}
		}
	}
	s.runStop(nil)
}

// runStop requests to stop Stage.Run; ev may be nil.
// If already stopping, waits until that is done.
func (s *StageBase) runStop(ev *pipe.Event) bool {
	if s == nil {
		return false
	} else if s.stopped.Swap(true) {
		<-s.stopdone
		return false
	} else {
		defer close(s.stopdone)
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
