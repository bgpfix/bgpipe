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

// Stage represents a bgpipe stage
type Stage struct {
	zerolog.Logger
	StageValue

	B *Bgpipe      // parent
	P *pipe.Pipe   // bgpfix pipe
	K *koanf.Koanf // integrated config

	Idx  int    // stage index
	Cmd  string // stage command name
	Name string // human-friendly stage name

	Flags    *pflag.FlagSet // CLI flags
	Usage    string         // CLI stage s.Usage string
	Argnames []string       // CLI argument names for exporting to K

	IsLeft      bool // operates on L direction?
	IsRight     bool // operates on R direction?
	IsReader    bool // reads Direction.Out?
	IsRawReader bool // uses Direction.Read?
	IsWriter    bool // writes Direction.In?
	IsRawWriter bool // uses Direction.Write?
}

// StageValue implements the actual work
type StageValue interface {
	// Prepare checks config and prepares for Start.
	Prepare() error

	// Start is run as a goroutine after the pipe starts.
	// Returning a non-nil error results in a fatal error.
	Start() error
}

// NewStageFunc returns a new Stage for name cmd and position idx.
type NewStageFunc func(s *Stage) StageValue

// NewStageFuncs maps stage commands to corresponding NewStageFunc
var NewStageFuncs = map[string]NewStageFunc{
	"connect": NewTcpConnect,
	"tcp":     NewTcpConnect,
	"mrt":     NewMrt,
}

// NewStage adds and returns a new stage at idx for cmd,
// or returns an existing instance if it's for the same cmd.
func (b *Bgpipe) NewStage(idx int, cmd string) (*Stage, error) {
	// already there? check cmd
	if idx < len(b.Stage2) {
		if s := b.Stage2[idx]; s != nil {
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
	s := &Stage{}
	s.B = b
	s.P = b.Pipe
	s.K = koanf.New(".")
	s.Idx = idx
	s.Cmd = cmd
	s.SetName(fmt.Sprintf("[%d] %s", idx, cmd))

	// common CLI flags
	s.Flags = pflag.NewFlagSet(cmd, pflag.ExitOnError)
	s.Flags.SortFlags = false
	s.Flags.BoolP("left", "L", false, "L direction")
	s.Flags.BoolP("right", "R", false, "R direction")

	// create sv
	s.StageValue = newfunc(s)

	// store
	for idx >= len(b.Stage2) {
		b.Stage2 = append(b.Stage2, nil)
	}
	b.Stage2[idx] = s
	b.Last = len(b.Stage2) - 1

	return s, nil
}

// SetName updates s.Name and s.Logger
func (s *Stage) SetName(name string) {
	s.Name = name
	s.Logger = s.B.With().Str("stage", s.Name).Logger()
}

// ParseArgs parses CLI flags and arguments, exporting to K
func (s *Stage) ParseArgs(args []string) error {
	// override s.Flags.Usage?
	if s.Flags.Usage == nil {
		if len(s.Usage) == 0 {
			s.Usage = strings.ToUpper(strings.Join(s.Argnames, " "))
		}
		s.Flags.Usage = func() {
			fmt.Fprintf(os.Stderr, "Stage usage: %s %s\n", s.Cmd, s.Usage)
			fmt.Fprint(os.Stderr, s.Flags.FlagUsages())
		}
	}

	// parse stage args
	if err := s.Flags.Parse(args); err != nil {
		return fmt.Errorf("%s: %w", s.Name, err)
	}

	// export to koanf
	s.K.Load(posflag.Provider(s.Flags, ".", s.K), nil)

	sargs := s.Flags.Args()
	s.K.Set("args", sargs)
	for i, name := range s.Argnames {
		if i < len(sargs) {
			s.K.Set(name, sargs[i])
		} else {
			break
		}
	}

	return nil
}

// Prepare wraps StageValue.Prepare and adds some logic around config
func (s *Stage) Prepare() error {
	k := s.K

	// check direction settings
	switch left, right := k.Bool("left"), k.Bool("right"); {
	case s.IsFirst():
		if left {
			return ErrFirstL
		}
		s.IsRight = true // force R direction

	case s.IsLast():
		if right {
			return ErrLastR
		}
		s.IsLeft = true // force L direction

	default:
		if left || right {
			s.IsLeft = left
			s.IsRight = right
		} else {
			s.IsRight = true // by default send to R
		}
	}

	// call child prepare
	if err := s.StageValue.Prepare(); err != nil {
		return err
	}

	// fix I/O settings
	s.IsReader = s.IsReader || s.IsRawReader
	s.IsWriter = s.IsWriter || s.IsRawWriter

	// needs raw access?
	if s.IsRawReader || s.IsRawWriter {
		if !s.IsFirst() && !s.IsLast() {
			return ErrFirstOrLast
		}
	}

	return nil
}

// Start starts StageValue.Start and waits till finish.
// Cancels the main bgpipe context on error.
// Respects b.wg_* waitgroups.
func (s *Stage) Start() {
	b := s.B

	err := s.StageValue.Start()
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

func (s *Stage) IsLReader() bool {
	return s.IsRight && s.IsReader
}

func (s *Stage) IsRReader() bool {
	return s.IsLeft && s.IsReader
}

func (s *Stage) IsLWriter() bool {
	return s.IsLeft && s.IsWriter
}

func (s *Stage) IsRWriter() bool {
	return s.IsRight && s.IsWriter
}

// Errorf wraps fmt.Errorf and adds a prefix with the stage name
func (s *Stage) Errorf(format string, a ...any) error {
	return fmt.Errorf(s.Name+": "+format, a...)
}

func (s *Stage) IsFirst() bool {
	return s.Idx == 0
}

func (s *Stage) IsLast() bool {
	return s.Idx == s.B.Last
}

// Dst returns the pipe direction which the stage should write to
func (s *Stage) Dst() *pipe.Direction {
	if s.IsLeft {
		return s.P.L
	} else {
		return s.P.R
	}
}

// Src returns the pipe direction which the stage should read from
func (s *Stage) Src() *pipe.Direction {
	if s.IsLeft {
		return s.P.R
	} else {
		return s.P.L
	}
}
