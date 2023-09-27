package bgpipe

import (
	"fmt"
	"math"
	"slices"

	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/pipe"
)

// pipePrepare prepares the bgpipe
func (b *Bgpipe) pipePrepare() error {
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

	// prepare stages
	has_stdout := false
	for _, s := range b.Stages {
		if s == nil {
			continue
		}
		if err := s.stagePrepare(); err != nil {
			return s.Errorf("%w", err)
		}
		if s.Cmd == "stdout" {
			has_stdout = true
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
	if !k.Bool("quiet") {
		if !has_stdout {
			s := b.NewStage("stdout")
			s.K.Set("auto", true)
			s.Stage.Prepare()
		}
	}

	return nil
}

// stagePrepare wraps Stage.Prepare and adds some logic around config
func (s *StageBase) stagePrepare() error {
	s.Debug().Interface("koanf", s.K.All()).Msg("preparing")

	// first / last?
	s.IsFirst = s.Index == 0
	s.IsLast = s.Index == len(s.B.Stages)-1

	// direction settings
	s.IsLeft = s.K.Bool("left")
	s.IsRight = s.K.Bool("right")
	if !s.IsLeft && !s.IsRight {
		if s.IsLast {
			s.IsLeft = true // last? by default send to L <-
		} else {
			s.IsRight = true // first? by default send to -> R
		}
	}

	// call child prepare
	po := &s.P.Options
	cbs := len(po.Callbacks)
	hds := len(po.Handlers)
	if err := s.Stage.Prepare(); err != nil {
		return err
	}

	// make stage callbacks and handlers depend on s.enabled
	s.Callbacks = po.Callbacks[cbs:]
	for _, cb := range s.Callbacks {
		cb.Index = s.Index
		cb.Enabled = &s.enabled
	}
	s.Handlers = po.Handlers[hds:]
	for _, h := range s.Handlers {
		h.Index = s.Index
		h.Enabled = &s.enabled
	}

	// where to inject new messages?
	switch s.K.String("in") {
	case "here":
		s.CallbackIndex = s.Index
	case "after":
		if s.IsLeft {
			s.CallbackIndex = s.Index - 1
		} else {
			s.CallbackIndex = s.Index + 1
		}
	case "first":
		if s.IsLeft {
			s.CallbackIndex = math.MaxInt
		} else {
			s.CallbackIndex = math.MinInt
		}
	case "last":
		if s.IsLeft {
			s.CallbackIndex = math.MinInt
		} else {
			s.CallbackIndex = math.MaxInt
		}
	}

	// fix I/O settings
	s.IsConsumer = s.IsConsumer || s.IsReader
	s.IsProducer = s.IsProducer || s.IsWriter

	// needs stream access?
	if s.IsReader || s.IsWriter {
		if !(s.IsFirst || s.IsLast) {
			return ErrFirstOrLast
		}
	}

	// has trigger-on events?
	if evs := s.cfgEvents("on"); len(evs) > 0 {
		s.enabled.Store(false)
		s.P.Options.OnEventFirst(s.startEvent, evs...)

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
		s.P.Options.OnEventLast(s.stopEvent, evs...)
	}

	return nil
}
