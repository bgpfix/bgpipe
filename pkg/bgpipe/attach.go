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
	if b.StageCount() < 1 {
		b.F.Usage()
		return fmt.Errorf("bgpipe needs at least 1 stage")
	}

	// reverse the pipe?
	if k.Bool("reverse") {
		slices.Reverse(b.Stages[1:])
		for idx, s := range b.Stages {
			if s == nil {
				continue
			}
			s.Index = idx

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
	if !k.Bool("silent") && !has_stdout {
		s := b.NewStage("stdout")
		s.K.Set("left", true)
		s.K.Set("right", true)
		if err := s.attach(); err != nil {
			return fmt.Errorf("auto stdout: %w", err)
		}
	}

	// add automatic stdin?
	if k.Bool("stdin") && !has_stdin {
		s := b.NewStage("stdin")
		s.K.Set("left", true)
		s.K.Set("right", true)
		s.K.Set("in", "first")
		s.K.Set("wait", "ESTABLISHED")
		if err := s.attach(); err != nil {
			return fmt.Errorf("auto stdin: %w", err)
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

	// first / last?
	if s.Index == 1 {
		s.IsFirst = true
	} else if s.Index == b.StageCount() {
		s.IsLast = true
	}

	// left / right?
	s.IsLeft = k.Bool("left")
	s.IsRight = k.Bool("right")
	if s.IsLeft && s.IsRight {
		if !s.Options.AllowLR {
			return ErrLR
		}
	} else if s.IsLeft == s.IsRight { // both false
		s.IsRight = true // the default

		// exceptions
		if s.IsLast && s.Options.IsProducer {
			s.IsRight = false
		} else if s.IsFirst && !s.Options.IsProducer {
			s.IsRight = false
		}

		// symmetry
		s.IsLeft = !s.IsRight
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

	// if not an internal stage...
	if s.Index > 0 {
		// update the logger
		name := s.Name
		if name[0] != '[' {
			name = fmt.Sprintf("[%d] %s", s.Index, name)
		}
		s.Logger = s.B.With().Str("stage", name).Logger()

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
			cb.Enabled = &s.running
		}
		s.Handlers = po.Handlers[hds:]
		for _, h := range s.Handlers {
			h.Id = s.Index
			h.Enabled = &s.running
		}
	}

	// update related waitgroups
	s.wgAdd(1)

	// has trigger-on events?
	if evs := b.parseEvents(k, "wait"); len(evs) > 0 {
		po.OnEventPre(s.runStart, evs...)

		// re-target pipe.EVENT_START handlers to the --wait events
		for _, h := range s.Handlers {
			for i, t := range h.Types {
				if t == pipe.EVENT_START {
					h.Types[i] = evs[0]
					h.Types = append(h.Types, evs[1:]...)
				}
			}
		}
	} else {
		po.OnEventPre(s.runStart, pipe.EVENT_START)
	}

	// has trigger-off events?
	if evs := b.parseEvents(k, "stop"); len(evs) > 0 {
		po.OnEventPost(s.runStop, evs...)
	}

	s.Trace().Msgf("attached [%d] first/last=%v/%v L/R=%v,%v startat=%d",
		s.Index, s.IsFirst, s.IsLast, s.IsLeft, s.IsRight, s.StartAt)

	return nil
}
