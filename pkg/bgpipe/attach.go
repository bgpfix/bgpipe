package bgpipe

import (
	"fmt"
	"slices"

	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/pipe"
)

// attach prepares the bgpipe by attaching stages to pipe
func (b *Bgpipe) attach() error {
	// shortcuts
	var (
		k  = b.K
		p  = b.Pipe
		po = &p.Options
	)

	// at least one stage defined?
	if len(b.Stages) == 0 {
		b.F.Usage()
		return fmt.Errorf("bgpipe needs at least 1 stage")
	}

	// reverse?
	if k.Bool("reverse") {
		slices.Reverse(b.Stages)
		for idx, s := range b.Stages {
			if s != nil {
				s.Index = idx
				s.SetName(fmt.Sprintf("[%d] %s", idx, s.Cmd))
			}
		}
		left, right := k.Bool("left"), k.Bool("right")
		k.Set("left", right)
		k.Set("right", left)
	}

	// attach stages
	var (
		has_stdin  int
		has_stdout int
	)
	for idx, s := range b.Stages {
		if s == nil {
			continue
		}

		if err := s.attach(); err != nil {
			return s.Errorf("%w", err)
		}

		// cross-pipe checks
		if s.Options.IsStdin {
			if has_stdin > 0 {
				s.Warn().Msgf("stage %s already uses stdin", b.Stages[has_stdin].Name)
			} else {
				has_stdin = idx
			}
		}
		if s.Options.IsStdout {
			if has_stdout > 0 {
				s.Warn().Msgf("stage %s already uses stdout", b.Stages[has_stdout].Name)
			} else {
				has_stdout = idx
			}
		}
	}

	// force 2-byte ASNs?
	if k.Bool("short-asn") {
		p.Caps.Set(caps.CAP_AS4, nil) // ban CAP_AS4
	} else {
		p.Caps.Use(caps.CAP_AS4) // use CAP_AS4 by default
	}

	// attach to events
	po.OnStart(b.onStart)
	if !k.Bool("perr") {
		po.OnParseError(b.onParseError) // pipe.EVENT_PARSE
	}

	// add automatic stdout?
	if !k.Bool("silent") && has_stdout == 0 {
		s := b.NewStage("stdout")
		s.K.Set("auto", true)
		s.Stage.Attach()
	}

	return nil
}

// attach wraps Stage.Attach and adds some logic
func (s *StageBase) attach() error {
	s.Debug().Interface("koanf", s.K.All()).Msg("preparing")

	// first / last?
	s.IsFirst = s.Index == 1
	s.IsLast = s.Index == len(s.B.Stages)-1

	// direction settings
	s.IsLeft = s.K.Bool("left")
	s.IsRight = s.K.Bool("right")
	if !s.IsLeft && !s.IsRight {
		if s.IsFirst {
			s.IsRight = true // first? by default send to -> R
		} else if s.IsLast {
			s.IsLeft = true // last? by default send to L <-
		} else {
			s.IsRight = true // in the middle = by default -> R
		}
	}

	// where to inject new messages?
	switch v := s.K.String("in"); v {
	case "here":
		s.StartAt = s.Index
	case "first", "":
		s.StartAt = 0
	case "last":
		s.StartAt = -1
	default:
		return fmt.Errorf("%w: %s", ErrInject, v)
	}

	// call child attach
	po := &s.P.Options
	cbs := len(po.Callbacks)
	hds := len(po.Handlers)
	if err := s.Stage.Attach(); err != nil {
		return err
	}

	// needs raw stream access?
	if s.Options.IsRawReader || s.Options.IsRawWriter {
		if !(s.IsFirst || s.IsLast) {
			return ErrFirstOrLast
		}
	}

	// make stage callbacks and handlers depend on s.enabled
	s.Callbacks = po.Callbacks[cbs:]
	for _, cb := range s.Callbacks {
		cb.Id = s.Index
		cb.Enabled = &s.enabled
	}
	s.Handlers = po.Handlers[hds:]
	for _, h := range s.Handlers {
		h.Id = s.Index
		h.Enabled = &s.enabled
	}

	// has trigger-on events?
	if evs := s.cfgEvents("on"); len(evs) > 0 {
		s.enabled.Store(false)
		po.OnEventPre(s.startEvent, evs...)

		// re-target pipe.EVENT_START handlers to --on events
		for _, h := range s.Handlers {
			for i, t := range h.Types {
				if t == pipe.EVENT_START {
					h.Types[i] = evs[0]
					h.Types = append(h.Types, evs[1:]...)
				}
			}
		}
	}

	// has trigger-off events?
	if evs := s.cfgEvents("off"); len(evs) > 0 {
		po.OnEventPost(s.stopEvent, evs...)
	}

	return nil
}

// Attach is the default Stage implementation that does nothing
func (s *StageBase) Attach() error {
	return nil
}
