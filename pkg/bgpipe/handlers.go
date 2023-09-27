package bgpipe

import "github.com/bgpfix/bgpfix/pipe"

// onStart is called after the bgpfix pipe starts
func (b *Bgpipe) onStart(ev *pipe.Event) bool {
	// go through all stages
	for _, s := range b.Stages {
		if s == nil {
			continue
		}

		// kick waitgroups
		if s.isLReader() {
			b.wg_lread.Add(1)
		}
		if s.isLWriter() {
			b.wg_lwrite.Add(1)
		}
		if s.isRReader() {
			b.wg_rread.Add(1)
		}
		if s.isRWriter() {
			b.wg_rwrite.Add(1)
		}

		if s.enabled.Load() {
			go s.run()
		}
	}

	// wait for L/R writers
	go func() {
		b.wg_lwrite.Wait()
		b.Debug().Msg("closing L input")
		b.Pipe.L.CloseInput()
	}()
	go func() {
		b.wg_rwrite.Wait()
		b.Debug().Msg("closing R input")
		b.Pipe.R.CloseInput()
	}()

	// wait for L/R readers
	go func() {
		b.wg_lread.Wait()
		b.Debug().Msg("closing L output")
		b.Pipe.L.CloseOutput()
	}()
	go func() {
		b.wg_rread.Wait()
		b.Debug().Msg("closing R output")
		b.Pipe.R.CloseOutput()
	}()

	return false
}

// onParseError is called when the pipe sees a message it cant parse
func (b *Bgpipe) onParseError(ev *pipe.Event) bool {
	b.Error().
		Str("msg", ev.Msg.String()).
		Err(ev.Error).
		Msg("message parse error")
	return true
}

// startEvent starts the stage in reaction to a pipe event
func (s *StageBase) startEvent(ev *pipe.Event) (keep_event bool) {
	if s.Ctx.Err() != nil {
		return false // already stopped
	} else if !s.enabled.CompareAndSwap(false, true) {
		return false // already started
	}

	s.Debug().Msgf("start event %s", ev.Type)
	go s.run()
	return false
}

// stopEvent stops the stage in reaction to a pipe event
func (s *StageBase) stopEvent(ev *pipe.Event) (keep_event bool) {
	if s.Ctx.Err() != nil {
		return false // already stopped
	} else if !s.enabled.CompareAndSwap(true, false) {
		return true // just stopped or not started yet
	}

	s.Debug().Msgf("stop event %s", ev.Type)
	s.Cancel(ErrStageStopped)
	return false
}
