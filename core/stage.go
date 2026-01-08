package core

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/filter"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

// Stage implements a bgpipe stage
type Stage interface {
	// Attach is run before the pipe starts.
	// It should check the config and attach to the bgpfix pipe.
	Attach() error

	// Prepare is called when the stage starts, but before Run, callbacks, and handlers.
	// It should prepare required I/O, eg. files, network connections, etc.
	//
	// If no error is returned, the stage emits a "READY" event, all callbacks and handlers
	// are enabled, and Run is called when all stages starting in parallel are ready too.
	Prepare() error

	// Run runs the stage and returns after all work has finished.
	// It must respect StageBase.Ctx. Returning a non-nil error different
	// than ErrStopped results in a fatal error that stops the whole pipe.
	//
	// Emits "START" just before, and "STOP" after stage operation is finished.
	Run() error

	// Stop is called when the stage is requested to stop (by an event),
	// or after Run() exits (in order to clean-up).
	// It should safely finish all I/O and make Run return if it's still running.
	Stop() error
}

// StageOptions describe high-level settings of a stage
type StageOptions struct {
	Descr  string            // one-line description
	Flags  *pflag.FlagSet    // CLI flags
	Usage  string            // usage string
	Args   []string          // required argument names
	Events map[string]string // event names and descriptions

	// these can be modified before Attach(), and even inside (with care)

	IsProducer bool // produces messages? (= writes Line.Input?)
	IsConsumer bool // consumes messages? (= reads Line.Out?)
	IsStdin    bool // reads from stdin?
	IsStdout   bool // writes to stdout?
	Bidir      bool // allow -LR (bidir mode)?
	FilterIn   bool // allow stage input filtering? (must have callbacks)
	FilterOut  bool // allow stage output filtering? (must have inputs)
}

// StageBase represents a bgpipe stage base
type StageBase struct {
	zerolog.Logger // logger with stage name
	Stage          // the real implementation

	started atomic.Bool   // true if already started
	stopped atomic.Bool   // true if already stopped
	running atomic.Bool   // true if stage running
	done    chan struct{} // closed when Run returns

	Ctx    context.Context         // stage context
	Cancel context.CancelCauseFunc // cancel to stop the stage

	B *Bgpipe      // parent
	P *pipe.Pipe   // bgpfix pipe
	K *koanf.Koanf // integrated config (args / config file / etc)

	Index   int          // stage index (zero means internal)
	Cmd     string       // stage command name
	Name    string       // human-friendly stage name
	Flags   []string     // consumed flags
	Args    []string     // consumed args
	Options StageOptions // stage options

	flt_in  *filter.Filter // message filter for callbacks
	flt_out *filter.Filter // message filter for inputs

	// properties set during Attach()

	IsFirst bool    // is the first stage in pipe? (the L peer)
	IsLast  bool    // is the last stage in pipe? (the R peer)
	IsRight bool    // write L->R msgs + capture L->R msgs?
	IsLeft  bool    // write R->L msgs + capture R->L msgs?
	IsBidir bool    // true iff IsRight && IsLeft
	Dir     dir.Dir // target direction (IsLeft/IsRight translated, can be DIR_LR)

	callbacks []*pipe.Callback // registered callbacks
	handlers  []*pipe.Handler  // registered handlers
	inputs    []*pipe.Input    // registered inputs
}

// Attach is the default Stage implementation that does nothing.
func (s *StageBase) Attach() error {
	return nil
}

// Prepare is the default Stage implementation that does nothing.
func (s *StageBase) Prepare() error {
	return nil
}

// Run is the default Stage implementation that just waits
// for the context and returns its cancel cause
func (s *StageBase) Run() error {
	<-s.Ctx.Done()
	return context.Cause(s.Ctx)
}

// Stop is the default Stage implementation that does nothing.
func (s *StageBase) Stop() error {
	return nil
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
	s.Name = cmd
	s.Logger = s.B.With().Str("stage", s.Name).Logger()
	s.done = make(chan struct{})

	// CLI flags
	so := &s.Options
	so.Flags = pflag.NewFlagSet(cmd, pflag.ExitOnError)
	f := so.Flags
	f.SortFlags = false
	f.SetInterspersed(false)

	// create s, which should add specific CLI flags
	s.Stage = newfunc(s)

	// add global CLI flags
	f.BoolP("left", "L", false, "operate in the L direction")
	f.BoolP("right", "R", false, "operate in the R direction")
	f.BoolP("args", "A", false, "consume all CLI arguments till --")
	f.StringSliceP("wait", "W", []string{}, "wait for given event before starting")
	f.StringSliceP("stop", "S", []string{}, "stop after given event is handled")
	if so.IsProducer {
		f.StringP("new", "N", "next", "which stage to send new messages to")
	}
	if so.FilterOut {
		f.StringP("of", "O", "", "stage output filter (drop non-matching output)")
	}
	if so.FilterIn {
		f.StringP("if", "I", "", "stage input filter (skip non-matching input)")
	}
	f.Float64("limit-rate", 0, "message processing rate limit")
	f.Bool("limit-sample", false, "drop messages over the rate limit instead of delaying")

	return s
}

// wgAdd adds delta to B.wg* waitgroups related to s
func (s *StageBase) wgAdd(delta int) {
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

// Event sends an event, prefixing et with stage name + slash
func (s *StageBase) Event(et string, args ...any) *pipe.Event {
	return s.B.Pipe.Event(s.Name+"/"+et, append(args, s)...)
}

// Running returns true if the stage is in Run(), false otherwise.
func (s *StageBase) Running() bool {
	return s.running.Load()
}

// String returns stage "[index] name" or "name" if index is 0
func (s *StageBase) String() string {
	if s.Index != 0 {
		return fmt.Sprintf("[%d] %s", s.Index, s.Name)
	} else {
		return s.Name
	}
}

// StringDir returns eg. "-LR [FIRST]" depending on stage direction
func (s *StageBase) StringLR() string {
	var str strings.Builder
	str.WriteByte('-')
	if s.IsLeft {
		str.WriteByte('L')
	}
	if s.IsRight {
		str.WriteByte('R')
	}
	if s.IsFirst {
		str.WriteString(" [FIRST]")
	}
	if s.IsLast {
		str.WriteString(" [LAST]")
	}
	return str.String()
}
