package bgpipe

import (
	"fmt"
	"os"
	"strings"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

// StageBase represents a bgpipe stage base
type StageBase struct {
	zerolog.Logger
	Stage

	B *Bgpipe      // parent
	P *pipe.Pipe   // bgpfix pipe
	K *koanf.Koanf // integrated config

	Idx  int    // stage index
	Cmd  string // stage command name
	Name string // human-friendly stage name

	Flags *pflag.FlagSet // CLI flags
	Usage string         // CLI stage s.Usage string
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
}

// Stage implements a bgpipe stage
type Stage interface {
	// Prepare checks config and prepares for Start.
	// It should modify parent's Is(Raw)Reader/Writer settings.
	Prepare() error

	// Start is run as a goroutine after the pipe starts.
	// Returning a non-nil error results in a fatal error.
	Start() error
}

// NewStageFunc returns a new Stage for given parent base
type NewStageFunc func(base *StageBase) Stage

// NewStageFuncs maps stage commands to corresponding NewStageFunc
var NewStageFuncs = map[string]NewStageFunc{
	"tcp":     NewTcpConnect,
	"mrt":     NewMrt,
	"speaker": NewSpeaker,
}

// GetStage adds and returns a new stage at idx for cmd,
// or returns an existing instance if it's for the same cmd.
func (b *Bgpipe) GetStage(idx int, cmd string) (*StageBase, error) {
	// already there? check cmd
	if idx < len(b.Stages) {
		if s := b.Stages[idx]; s != nil {
			if cmd == "" || s.Cmd == cmd {
				return s, nil
			} else {
				return nil, fmt.Errorf("[%d] %s: %w: %s", idx, cmd, ErrStageDiff, s.Cmd)
			}
		}
	}

	// cmd valid?
	newfunc, ok := NewStageFuncs[cmd]
	if !ok {
		return nil, fmt.Errorf("[%d] %s: %w", idx, cmd, ErrStageCmd)
	}

	// create new stage
	s := &StageBase{}
	s.B = b
	s.P = b.Pipe
	s.K = koanf.New(".")
	s.Idx = idx
	s.Cmd = cmd
	s.SetName(fmt.Sprintf("[%d] %s", idx, cmd))

	// common CLI flags
	s.Flags = pflag.NewFlagSet(cmd, pflag.ExitOnError)
	s.Flags.SetInterspersed(false)
	s.Flags.SortFlags = false
	s.Flags.BoolP("left", "L", false, "L direction")
	s.Flags.BoolP("right", "R", false, "R direction")
	s.Flags.Bool("wait", false, "wait for ESTABLISHED")

	// create sv
	s.Stage = newfunc(s)

	// store
	for idx >= len(b.Stages) {
		b.Stages = append(b.Stages, nil)
	}
	b.Stages[idx] = s

	return s, nil
}

// SetName updates s.Name and s.Logger
func (s *StageBase) SetName(name string) {
	s.Name = name
	s.Logger = s.B.With().Str("stage", s.Name).Logger()
}

// ParseArgs parses CLI flags and arguments, exporting to K.
// May return unused args.
func (s *StageBase) ParseArgs(args []string) ([]string, error) {
	// override s.Flags.Usage?
	if s.Flags.Usage == nil {
		if len(s.Usage) == 0 {
			s.Usage = strings.ToUpper(strings.Join(s.Args, " "))
		}
		s.Flags.Usage = func() {
			fmt.Fprintf(os.Stderr, "Stage usage: %s %s\n", s.Cmd, s.Usage)
			fmt.Fprint(os.Stderr, s.Flags.FlagUsages())
		}
	}

	// parse stage flags, export to koanf
	if err := s.Flags.Parse(args); err != nil {
		return args, s.Errorf("%w", err)
	} else {
		s.K.Load(posflag.Provider(s.Flags, ".", s.K), nil)
	}

	// uses CLI arguments?
	sargs := s.Flags.Args()
	if s.Args != nil {
		// special case: all arguments
		if len(s.Args) == 0 {
			s.K.Set("args", sargs)
			return nil, nil
		}

		// rewrite into k
		for _, name := range s.Args {
			if len(sargs) == 0 || sargs[0] == "--" {
				return sargs, s.Errorf("needs an argument: %s", name)
			}
			s.K.Set(name, sargs[0])
			sargs = sargs[1:]
		}
	}

	// drop explicit --
	if len(sargs) > 0 && sargs[0] == "--" {
		sargs = sargs[1:]
	}

	return sargs, nil
}

// Prepare wraps Stage.Prepare and adds some logic around config
func (s *StageBase) Prepare() error {
	k := s.K
	s.Debug().Interface("koanf", k.All()).Msg("preparing")

	// double-check direction settings
	s.IsLeft, s.IsRight = k.Bool("left"), k.Bool("right")
	switch s.Idx {
	case 0:
		if s.IsLeft {
			return ErrFirstL
		}
		s.IsRight = true // force R direction
		s.IsFirst = true
	case len(s.B.Stages) - 1:
		if s.IsRight {
			return ErrLastR
		}
		s.IsLeft = true // force L direction
		s.IsLast = true
	default:
		if !(s.IsLeft || s.IsRight) {
			s.IsRight = true // by default send to R
		}
	}

	// call child prepare
	if err := s.Stage.Prepare(); err != nil {
		return err
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

	return nil
}

// Start starts Stage.Start and waits till finish.
// Cancels the main bgpipe context on error.
// Respects b.wg_* waitgroups.
func (s *StageBase) Start() {
	s.Debug().Msg("starting")

	b := s.B
	err := s.Stage.Start()
	if err != nil {
		b.cancel(s.Errorf("%w", err))
	}

	if s.IsLReader() {
		b.wg_lread.Done()
	}
	if s.IsLWriter() {
		b.wg_lwrite.Done()
	}
	if s.IsRReader() {
		b.wg_rread.Done()
	}
	if s.IsRWriter() {
		b.wg_rwrite.Done()
	}
}

// IsLReader returns true iff the stage is supposed to write L.In
func (s *StageBase) IsLWriter() bool {
	return s.IsLeft && s.IsWriter
}

// IsRWriter returns true iff the stage is supposed to write R.In
func (s *StageBase) IsRWriter() bool {
	return s.IsRight && s.IsWriter
}

// IsLReader returns true iff the stage is supposed to read L.Out
func (s *StageBase) IsLReader() bool {
	return s.IsRight && s.IsReader
}

// IsLReader returns true iff the stage is supposed to read R.Out
func (s *StageBase) IsRReader() bool {
	return s.IsLeft && s.IsReader
}

// Errorf wraps fmt.Errorf and adds a prefix with the stage name
func (s *StageBase) Errorf(format string, a ...any) error {
	return fmt.Errorf(s.Name+": "+format, a...)
}

// Upstream returns the direction which the stage should write to, if its unidirectional
func (s *StageBase) Upstream() *pipe.Direction {
	if s.IsLeft {
		return s.P.L
	} else {
		return s.P.R
	}
}

// Downstream returns the direction which the stage should read from, if its unidirectional
func (s *StageBase) Downstream() *pipe.Direction {
	if s.IsLeft {
		return s.P.R
	} else {
		return s.P.L
	}
}
