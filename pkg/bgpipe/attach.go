package bgpipe

import (
	"fmt"
	"math"
	"slices"

	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/pipe"
)

// Attach attaches all stages to pipe
func (b *Bgpipe) Attach() error {
	// shortcuts
	var (
		k = b.K
		p = b.Pipe
	)

	// at least one stage defined?
	if len(b.Stages) < 2 {
		b.F.Usage()
		return fmt.Errorf("bgpipe needs at least 1 stage")
	}

	// reverse the pipe?
	if k.Bool("reverse") {
		slices.Reverse(b.Stages)
		for idx, s := range b.Stages {
			if s == nil {
				continue
			}
			s.Index = idx
			s.SetName(fmt.Sprintf("[%d] %s", idx, s.Cmd))

			left, right := s.K.Bool("left"), s.K.Bool("right")
			s.K.Set("left", right)
			s.K.Set("right", left)
		}
	}

	// attach stages
	var (
		has_stdin  bool
		has_stdout bool
	)
	for _, s := range b.Stages {
		if s == nil {
			continue
		}

		// run stage attach
		if err := s.attach(); err != nil {
			return s.Errorf("%w", err)
		}

		// does stdin/stdout?
		has_stdin = has_stdin || s.Options.IsStdin
		has_stdout = has_stdout || s.Options.IsStdout
	}

	// add automatic stdout?
	if k.Bool("stdout") && !has_stdout {
		s := b.NewStage("stdout")
		s.K.Set("left", true)
		s.K.Set("right", true)
		if err := s.attach(); err != nil {
			return fmt.Errorf("auto stdout: %w", err)
		} else {
			b.auto_stdout = s
		}
	}

	// add automatic stdin?
	if k.Bool("stdin") && !has_stdin {
		s := b.NewStage("stdin")
		s.K.Set("left", true)
		s.K.Set("right", true)
		s.K.Set("in", "first")
		s.attach()
		if err := s.attach(); err != nil {
			return fmt.Errorf("auto stdin: %w", err)
		} else {
			b.auto_stdin = s
		}
	}

	// force 2-byte ASNs?
	if k.Bool("short-asn") {
		p.Caps.Set(caps.CAP_AS4, nil) // ban CAP_AS4
	} else {
		p.Caps.Use(caps.CAP_AS4) // use CAP_AS4 by default
	}

	// log events?
	if evs := b.parseEvents(k, "events"); len(evs) > 0 {
		p.Options.AddHandler(b.LogEvent, &pipe.Handler{
			Pre:   true,
			Order: math.MinInt,
			Types: evs,
		})
	}

	return nil
}

// attach wraps Stage.Attach and adds some logic
func (s *StageBase) attach() error {
	var (
		b  = s.B
		p  = s.P
		po = &p.Options
		k  = s.K
	)

	s.Debug().Interface("koanf", k.All()).Msg("preparing")

	// first / last?
	s.IsFirst = s.Index == 1
	s.IsLast = s.Index == len(b.Stages)-1

	// direction settings
	s.IsLeft = k.Bool("left")
	s.IsRight = k.Bool("right")
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
	switch v := k.String("in"); v {
	case "here":
		s.StartAt = s.Index
	case "first", "":
		s.StartAt = 0
	case "last":
		s.StartAt = -1
	default:
		// a stage name reference?
		if len(v) > 0 && v[0] == '@' {
			for _, s2 := range s.B.Stages {
				if s2 != nil && s2.Name == v {
					s.StartAt = s2.Index
					break
				}
			}
		}
		if s.StartAt == 0 {
			return fmt.Errorf("%w: %s", ErrInject, v)
		}
	}

	// call child attach
	cbs := len(po.Callbacks)
	hds := len(po.Handlers)
	if err := s.Stage.Attach(); err != nil {
		return err
	}

	// prefix the name?
	if s.Name[0] != '[' {
		s.Name = fmt.Sprintf("[%d] %s", s.Index, s.Name)
	}
	s.SetName(s.Name)

	// is an internal stage?
	if s.Index == 0 {
		return nil
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
	if evs := b.parseEvents(k, "on"); len(evs) > 0 {
		s.enabled.Store(false)
		po.OnEventPre(s.runOn, evs...)

		// re-target pipe.EVENT_START handlers to the --on events
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
	if evs := b.parseEvents(k, "off"); len(evs) > 0 {
		po.OnEventPost(s.runOff, evs...)
	}

	return nil
}

// Attach is the default Stage implementation that does nothing
func (s *StageBase) Attach() error {
	return nil
}
