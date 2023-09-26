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

	Idx  int    // stage index
	Cmd  string // stage command name
	name string // human-friendly stage name

	Flags *pflag.FlagSet // CLI flags
	Descr string         // CLI stage one-line description
	Usage string         // CLI stage usage string
	Args  []string       // CLI argument names for exporting to K

	// set by StageBase.Prepare

	IsLeft  bool // operates on L direction?
	IsRight bool // operates on R direction?
	IsFirst bool // is the first stage in pipe? (L peer)
	IsLast  bool // is the last stage in pipe? (R peer)

	// set by Stage.Prepare

	IsReader       bool // reads pipe.Direction.Out?
	IsStreamReader bool // needs pipe.Direction.Read?
	IsWriter       bool // writes pipe.Direction.In?
	IsStreamWriter bool // needs pipe.Direction.Write?

	enabled atomic.Bool // true if enabled (--on), false if disabled (--off)
	started atomic.Bool // true if already started (or stopped before start)
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
	s.Flags.SortFlags = false
	s.Flags.SetInterspersed(false)
	s.Flags.BoolP("left", "L", false, "L direction")
	s.Flags.BoolP("right", "R", false, "R direction")
	s.Flags.StringSlice("on", []string{}, "start on given event")
	s.Flags.StringSlice("off", []string{}, "stop on given event")
	// TODO: add --inject-first (by default inject here+1)

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

// SetName updates s.Name and s.Logger
func (s *StageBase) SetName(name string) {
	s.name = name
	s.Logger = s.B.With().Str("stage", s.name).Logger()
}

// isLWriter returns true iff the stage is supposed to write L.In
func (s *StageBase) isLWriter() bool {
	return s.IsLeft && s.IsWriter
}

// isRWriter returns true iff the stage is supposed to write R.In
func (s *StageBase) isRWriter() bool {
	return s.IsRight && s.IsWriter
}

// isLReader returns true iff the stage is supposed to read L.Out
func (s *StageBase) isLReader() bool {
	return s.IsRight && s.IsReader
}

// isRReader returns true iff the stage is supposed to read R.Out
func (s *StageBase) isRReader() bool {
	return s.IsLeft && s.IsReader
}

// Errorf wraps fmt.Errorf and adds a prefix with the stage name
func (s *StageBase) Errorf(format string, a ...any) error {
	return fmt.Errorf(s.name+": "+format, a...)
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
