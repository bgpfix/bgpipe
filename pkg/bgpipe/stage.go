package bgpipe

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

// StageBase represents a bgpipe stage base
type StageBase struct {
	zerolog.Logger // logger with stage name
	Stage          // the real implementation

	started atomic.Bool // true if already started
	stopped atomic.Bool // true if already stopped
	enabled atomic.Bool // true if enabled (--on), false if disabled (--off)

	Ctx    context.Context         // stage context
	Cancel context.CancelCauseFunc // cancel to stop the stage

	B *Bgpipe      // parent
	P *pipe.Pipe   // bgpfix pipe
	K *koanf.Koanf // integrated config (args / config file / etc)

	Index   int          // stage index (zero means internal)
	Cmd     string       // stage command name
	Name    string       // human-friendly stage name
	Options StageOptions // stage options, can be updated in NewStage

	// set during StageBase.attach

	IsFirst bool // is the first stage in pipe? (L peer)
	IsLast  bool // is the last stage in pipe? (R peer)
	IsLeft  bool // operates on L direction?
	IsRight bool // operates on R direction?

	StartAt   int              // which stage to start at when injecting new messages?
	Callbacks []*pipe.Callback // registered callbacks
	Handlers  []*pipe.Handler  // registered handlers
}

// StageOptions describe high-level settings of a stage
type StageOptions struct {
	Descr string         // one-line description
	Flags *pflag.FlagSet // CLI flags
	Usage string         // CLI usage string
	Args  []string       // CLI argument names

	IsConsumer  bool // consumes pipe.Direction.Out? (not callbacks)
	IsProducer  bool // produces pipe.Direction.In?
	IsRawReader bool // needs exclusive pipe.Direction.Read access?
	IsRawWriter bool // needs exclusive pipe.Direction.Write access?
	IsStdin     bool // reads from stdin?
	IsStdout    bool // writes to stdout?
	// TODO: allow L+R?
}

// Stage implements a bgpipe stage
type Stage interface {
	// Attach checks config and prepares for Run, attaching to the pipe.
	Attach() error

	// Run runs the stage and returns after all work has finished.
	// It must respect StageBase.Ctx. Returning a non-nil error different
	// than ErrStopped results in a fatal error that stops the whole pipe.
	Run() error
}

// NewStage returns a new Stage for given parent base. It should modify base.Options.
type NewStage func(base *StageBase) Stage

// NewStage returns new stage for given cmd, or nil on error
func (b *Bgpipe) NewStage(cmd string) *StageBase {
	// cmd valid?
	newfunc, ok := b.repo[cmd]
	if !ok {
		return nil
	}

	// create new stage
	s := &StageBase{}
	s.Ctx, s.Cancel = context.WithCancelCause(b.Ctx)
	s.B = b
	s.P = b.Pipe
	s.K = koanf.New(".")
	s.Cmd = cmd
	s.SetName(cmd)
	s.enabled.Store(true)

	// common CLI flags
	s.Options.Flags = pflag.NewFlagSet(cmd, pflag.ExitOnError)
	f := s.Options.Flags
	f.SortFlags = false
	f.SetInterspersed(false)
	f.BoolP("left", "L", false, "operate in L direction")
	f.BoolP("right", "R", false, "operate in R direction")
	f.StringSlice("on", []string{}, "start when given event is received")
	f.StringSlice("off", []string{}, "stop after given event is handled")
	f.String("in", "here", "where in the pipe to inject new messages (first/here/last)")

	// create s
	s.Stage = newfunc(s)

	// fix I/O settings
	s.Options.IsConsumer = s.Options.IsConsumer || s.Options.IsRawReader
	s.Options.IsProducer = s.Options.IsProducer || s.Options.IsRawWriter

	// raw writers can't set --in
	if s.Options.IsRawWriter {
		if o := f.Lookup("in"); o != nil {
			o.Hidden = true
			o.Value.Set("")
		}
	}

	return s
}

// NewMsg returns a new, empty message from pipe pool,
// but respecting --in stage options.
func (s *StageBase) NewMsg() *msg.Msg {
	m := s.P.Get()
	pc := pipe.Context(m)
	switch s.StartAt {
	case 0: // first
		pc.StartAt = 0
	case -1: // last
		pc.NoCallbacks()
	default: // here
		pc.StartAt = s.StartAt
	}
	return m
}

// SetName updates s.Name and s.Logger
func (s *StageBase) SetName(name string) {
	s.Name = name
	s.Logger = s.B.With().Str("stage", s.Name).Logger()
}

// WgAdd adds delta to B.wg* waitgroups related to s
func (s *StageBase) WgAdd(delta int) {
	o := &s.Options
	if s.IsRight {
		if o.IsProducer {
			s.B.wg_rwrite.Add(delta)
		}
		if o.IsConsumer {
			s.B.wg_lread.Add(delta)
		}
	}
	if s.IsLeft {
		if o.IsProducer {
			s.B.wg_lwrite.Add(delta)
		}
		if o.IsConsumer {
			s.B.wg_rread.Add(delta)
		}
	}
}

// Errorf wraps fmt.Errorf and adds a prefix with the stage name
func (s *StageBase) Errorf(format string, a ...any) error {
	return fmt.Errorf(s.Name+": "+format, a...)
}

func (s *StageBase) Dst() msg.Dst {
	if s.IsLeft {
		if s.IsRight {
			return msg.DST_LR
		} else {
			return msg.DST_L
		}
	} else if s.IsRight || !s.IsLast {
		return msg.DST_R
	} else {
		return msg.DST_L
	}
}

// Upstream returns the direction which the stage should write to, if its unidirectional
func (s *StageBase) Upstream() *pipe.Direction {
	if s.IsLeft {
		return s.P.L
	} else if s.IsRight || !s.IsLast {
		return s.P.R
	} else {
		return s.P.L
	}
}

// Downstream returns the direction which the stage should read from, if its unidirectional
func (s *StageBase) Downstream() *pipe.Direction {
	if s.IsLeft {
		return s.P.R
	} else if s.IsRight || !s.IsLast {
		return s.P.L
	} else {
		return s.P.R
	}
}
