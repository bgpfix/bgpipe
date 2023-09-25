package bgpipe

import (
	"fmt"
	"slices"

	"github.com/bgpfix/bgpfix/caps"
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
				s.Idx = idx
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

	// add automatic stdin/stdout?
	if !k.Bool("quiet") {
		if !has_stdout {
			s := b.NewStage("stdout")
			s.K.Set("last", true)
			s.Stage.Prepare()
		}
	}

	return nil
}

// stagePrepare wraps Stage.stagePrepare and adds some logic around config
func (s *StageBase) stagePrepare() error {
	s.Debug().Interface("koanf", s.K.All()).Msg("preparing")

	// first / last?
	s.IsFirst = s.Idx == 0
	s.IsLast = s.Idx == len(s.B.Stages)-1

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
	for _, cb := range po.Callbacks[cbs:] {
		cb.Enabled = &s.enabled
	}
	for _, h := range po.Handlers[hds:] {
		h.Enabled = &s.enabled
	}

	// fix I/O settings
	s.IsReader = s.IsReader || s.IsStreamReader
	s.IsWriter = s.IsWriter || s.IsStreamWriter

	// needs stream access?
	if s.IsStreamReader || s.IsStreamWriter {
		if !(s.IsFirst || s.IsLast) {
			return ErrFirstOrLast
		}
	}

	// has trigger-on events?
	if on := s.K.Strings("on"); len(on) > 0 {
		s.enabled.Store(false)
		s.P.Options.OnEventFirst(s.startEvent, on...)
	}

	// has trigger-off events?
	if off := s.K.Strings("off"); len(off) > 0 {
		s.P.Options.OnEventLast(s.stopEvent, off...)
	}

	return nil
}
