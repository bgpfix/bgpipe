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

	Ctx    context.Context
	Cancel context.CancelCauseFunc

	B *Bgpipe      // parent
	P *pipe.Pipe   // bgpfix pipe
	K *koanf.Koanf // integrated config

	Index int    // stage index
	Cmd   string // stage command name
	Name  string // human-friendly stage name

	Flags *pflag.FlagSet // CLI flags
	Descr string         // CLI stage one-line description
	Usage string         // CLI stage usage string
	Args  []string       // CLI argument names for exporting to K

	enabled atomic.Bool // true if enabled (--on), false if disabled (--off)
	started atomic.Bool // true if already started

	// set by StageBase.Prepare

	IsLeft  bool // operates on L direction?
	IsRight bool // operates on R direction?
	IsFirst bool // is the first stage in pipe? (L peer)
	IsLast  bool // is the last stage in pipe? (R peer)

	Callbacks     []*pipe.Callback // registered callbacks
	Handlers      []*pipe.Handler  // registered handlers
	CallbackIndex int              // which callback to start at when injecting?

	// set by Stage.Prepare

	IsConsumer bool // consumes pipe.Direction.Out?
	IsProducer bool // produces pipe.Direction.In?
	IsReader   bool // needs exclusive pipe.Direction.Read access?
	IsWriter   bool // needs exclusive pipe.Direction.Write access?
}

// Stage implements a bgpipe stage
type Stage interface {
	// Prepare checks config and prepares for Start,
	// eg. attaching own callbacks to the pipe.
	Prepare() error

	// Start runs the stage and returns after all work has finished.
	// It must respect StageBase.Ctx. Returning a non-nil error different
	// than ErrStopped results in a fatal error that stops the whole pipe.
	Start() error
}

// NewStage returns a new Stage for given parent base
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
	s.Flags = pflag.NewFlagSet(cmd, pflag.ExitOnError)
	f := s.Flags
	f.SortFlags = false
	f.SetInterspersed(false)
	f.BoolP("left", "L", false, "operate in L direction")
	f.BoolP("right", "R", false, "operate in R direction")
	f.StringSlice("on", []string{}, "start just after given event is received")
	f.StringSlice("off", []string{}, "stop just after given event is handled")
	f.String("in", "here", "where in the pipe to inject new messages (here/after/first/last)")

	// create s
	s.Stage = newfunc(s)
	return s
}

// Prepare is the default Stage implementation that does nothing
func (s *StageBase) Prepare() error {
	return nil
}

// Start is the default Stage implementation that just waits
// for the context and returns its cancel cause
func (s *StageBase) Start() error {
	<-s.Ctx.Done()
	return context.Cause(s.Ctx)
}

// NewMsg returns a new, empty message from pipe pool,
// but respecting common stage options.
func (s *StageBase) NewMsg() *msg.Msg {
	m := s.P.Get()
	pc := pipe.Context(m)
	pc.Reverse = s.IsLeft
	pc.Index = s.CallbackIndex
	return m
}

// SetName updates s.Name and s.Logger
func (s *StageBase) SetName(name string) {
	s.Name = name
	s.Logger = s.B.With().Str("stage", s.Name).Logger()
}

// isLWriter returns true iff the stage is supposed to write L.In
func (s *StageBase) isLWriter() bool {
	return s.IsLeft && s.IsProducer
}

// isRWriter returns true iff the stage is supposed to write R.In
func (s *StageBase) isRWriter() bool {
	return s.IsRight && s.IsProducer
}

// isLReader returns true iff the stage is supposed to read L.Out
func (s *StageBase) isLReader() bool {
	return s.IsRight && s.IsConsumer
}

// isRReader returns true iff the stage is supposed to read R.Out
func (s *StageBase) isRReader() bool {
	return s.IsLeft && s.IsConsumer
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
