package core

import (
	"fmt"
	"math"
	"strconv"

	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// AttachStages attaches all stages to pipe
func (b *Bgpipe) AttachStages() error {
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

	// attach stages
	var (
		stdin_stage  *StageBase
		stdout_stage *StageBase
		count_stage  int
	)
	for _, s := range b.Stages {
		if s == nil {
			continue
		}

		// run stage attach
		if err := s.attach(); err != nil {
			return s.Errorf("%w", err)
		} else {
			count_stage++
		}

		// does stdin/stdout?
		if s.Options.IsStdin && stdin_stage == nil {
			stdin_stage = s
		}
		if s.Options.IsStdout && stdout_stage == nil {
			stdout_stage = s
		}
	}

	// only 1 stage without I/O? add stdout automatically
	if count_stage == 1 && stdin_stage == nil && stdout_stage == nil {
		k.Set("stdout", true)
	}

	// add stdout?
	if k.Bool("stdout") || k.Bool("stdout-wait") {
		if stdout_stage != nil {
			return fmt.Errorf("could not use --stdout: stage '%s' already writes to stdout", stdout_stage)
		}

		s := b.NewStage("stdout")
		args := []string{"--left", "--right"}
		if k.Bool("stdout-wait") {
			args = append(args, "--wait=EOR")
		}
		if _, err := s.parseArgs(args); err != nil {
			return fmt.Errorf("--stdout: %w", err)
		} else if err := s.attach(); err != nil {
			return fmt.Errorf("--stdout: %w", err)
		}
	}

	// add stdin?
	if k.Bool("stdin") || k.Bool("stdin-wait") {
		if stdin_stage != nil {
			return fmt.Errorf("could not use --stdin: stage '%s' already reads from stdin", stdin_stage)
		}

		s := b.NewStage("stdin")
		args := []string{"--left", "--right", "--new=first"}
		if k.Bool("stdin-wait") {
			args = append(args, "--wait=ESTABLISHED")
		}
		if _, err := s.parseArgs(args); err != nil {
			return fmt.Errorf("--stdin: %w", err)
		} else if err := s.attach(); err != nil {
			return fmt.Errorf("--stdin: %w", err)
		}
	}

	// force 2-byte ASNs?
	if k.Bool("short-asn") {
		p.Caps.Set(caps.CAP_AS4, nil) // ban CAP_AS4
	} else {
		p.Caps.Use(caps.CAP_AS4) // use CAP_AS4 by default
	}
	if k.Bool("guess-asn") {
		p.Caps.Use(caps.CAP_AS_GUESS) // use pseudo-capability to guess ASN size
	}

	// log events?
	if evs := ParseEvents(k.Strings("events"), "START", "STOP", "READY", "PREPARE"); len(evs) > 0 {
		b.Debug().Strs("events", evs).Msg("monitored events will be logged")
		p.Options.AddHandler(b.LogEvent, &pipe.Handler{
			Pre:   true,
			Order: math.MinInt,
			Types: evs,
		})
	}

	// kill events?
	if evs := ParseEvents(k.Strings("kill"), "STOP"); len(evs) > 0 {
		b.Debug().Strs("events", evs).Msg("will kill the session on given events")
		p.Options.AddHandler(b.KillEvent, &pipe.Handler{
			Pre:   true,
			Order: math.MinInt + 1,
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

	// first / last? (or both if only 1 stage)
	if s.Index == 1 {
		s.IsFirst = true
	}
	if s.Index == b.StageCount() {
		s.IsLast = true
	}

	// left / right?
	s.IsLeft = k.Bool("left")
	s.IsRight = k.Bool("right")
	s.IsBidir = s.IsLeft && s.IsRight
	if !s.IsLeft && !s.IsRight { // no explicit dir = apply a default
		s.IsRight = true

		// exceptions
		if s.IsLast && s.Options.IsProducer {
			s.IsRight = false
		} else if s.IsFirst && !s.Options.IsProducer {
			s.IsRight = false
		}

		// symmetry
		s.IsLeft = !s.IsRight
	}

	// set s.Dir
	if s.IsBidir {
		s.Dir = dir.DIR_LR
	} else if s.IsLeft {
		s.Dir = dir.DIR_L
	} else {
		s.Dir = dir.DIR_R
	}

	// call child attach, collect what was attached to
	cbs := len(po.Callbacks)
	hds := len(po.Handlers)
	ins := len(po.Inputs)
	if err := s.Stage.Attach(); err != nil {
		return err
	}
	s.callbacks = po.Callbacks[cbs:]
	s.handlers = po.Handlers[hds:]
	s.inputs = po.Inputs[ins:]

	// can run in bidir mode?
	if s.IsBidir && !s.Options.Bidir {
		return ErrLR
	}

	// if not an internal stage...
	if s.Index > 0 {
		// update the logger
		s.Logger = s.B.With().Str("stage", s.String()).Logger()

		// consumes messages?
		if s.Options.IsConsumer {
			if !(s.IsFirst || s.IsLast) {
				return ErrFirstOrLast
			}
		}
	}

	// verify IsProcessor and IsProducer
	if s.Options.FilterIn && len(s.callbacks) == 0 {
		return ErrNoCallbacks
	}
	if s.Options.IsProducer && len(s.inputs) == 0 {
		return ErrNoInputs
	}
	if s.Options.FilterOut && len(s.inputs) == 0 {
		return ErrNoInputs
	}

	// callback rate limit?
	rr := k.Float64("limit-rate")
	rs := k.Bool("limit-sample")
	var rl *rate.Limiter
	if rr > 0 {
		rl = rate.NewLimiter(rate.Limit(rr), int(math.Ceil(rr)))
	}

	// fix callbacks
	for _, cb := range s.callbacks {
		cb.Id = s.Index
		cb.Enabled = &s.running
		cb.Filter = s.flt_in
		cb.LimitRate = rl
		cb.LimitSkip = rs
	}

	// fix event handlers
	for _, h := range s.handlers {
		h.Id = s.Index
		h.Enabled = &s.running
	}

	// where to inject new messages?
	var frev, ffwd pipe.CbFilterMode // input filter mode
	var fid int                      // input filter callback id
	switch v := k.String("new"); v {
	case "next", "":
		frev, ffwd = pipe.CBFILTER_GE, pipe.CBFILTER_LE
		fid = s.Index
	case "here":
		frev, ffwd = pipe.CBFILTER_GT, pipe.CBFILTER_LT
		fid = s.Index
	case "first":
		frev, ffwd = pipe.CBFILTER_NONE, pipe.CBFILTER_NONE
	case "last":
		frev, ffwd = pipe.CBFILTER_ALL, pipe.CBFILTER_ALL
	default:
		frev, ffwd = pipe.CBFILTER_GE, pipe.CBFILTER_LE
		if id, err := strconv.Atoi(v); err == nil {
			fid = id
		} else if len(v) > 0 && v[0] == '@' {
			// a stage name reference?
			for _, s2 := range s.B.Stages {
				if s2 != nil && s2.Name == v {
					fid = s2.Index
					break
				}
			}
		}
		if fid <= 0 {
			return fmt.Errorf("%w: %s", ErrInject, v)
		}
	}

	// input rate limit? NB: separate from the callback rate limit
	if rr > 0 {
		rl = rate.NewLimiter(rate.Limit(rr), int(math.Ceil(rr)))
	}

	// fix inputs
	for _, li := range s.inputs {
		li.Id = s.Index
		li.CbFilterValue = fid
		li.Filter = s.flt_out
		li.LimitRate = rl
		li.LimitSkip = rs

		if li.Dir == dir.DIR_L {
			li.Reverse = true // CLI gives L stages in reverse
			li.CbFilter = frev
		} else {
			li.Reverse = false
			li.CbFilter = ffwd
		}
	}

	// is really a producer?
	has_inputs := len(s.inputs) > 0
	if s.Options.IsProducer != has_inputs {
		s.Debug().Msgf("IsProducer=%v but has_inputs=%v - correcting", s.Options.IsProducer, has_inputs)
		s.Options.IsProducer = has_inputs
	}

	// update related waitgroups
	s.wgAdd(1)

	// has trigger-on events?
	if evs := ParseEvents(k.Strings("wait"), "START"); len(evs) > 0 {
		s.Debug().Strs("events", evs).Msg("waiting for given events before start")
		po.OnEventPre(s.runStart, evs...)

		// trigger pipe start handlers by --wait events
		for _, h := range s.handlers {
			for _, t := range h.Types {
				if t == pipe.EVENT_START {
					h.Types = append(h.Types, evs...)
				}
			}
		}
	} else {
		po.OnEventPre(s.runStart, pipe.EVENT_START)
	}

	// has trigger-off events?
	if evs := ParseEvents(k.Strings("stop"), "STOP"); len(evs) > 0 {
		s.Debug().Strs("events", evs).Msg("will stop after given events")
		po.OnEventPost(s.runStop, evs...)
	}

	// debug?
	s.Debug().Msgf("[%d] attached %s %s", s.Index, s.Cmd, s.StringLR())
	if s.GetLevel() <= zerolog.TraceLevel {
		for _, cb := range s.callbacks {
			s.Trace().Msgf("  callback %#v", cb)
		}
		for _, hd := range s.handlers {
			s.Trace().Msgf("  handler %#v", hd)
		}
		for _, in := range s.inputs {
			s.Trace().Msgf("  input %s dir=%s reverse=%v filt=%d filt_id=%d",
				in.Name, in.Dir, in.Reverse, in.CbFilter, in.CbFilterValue)
		}
	}

	return nil
}
