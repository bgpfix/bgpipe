package bgpipe

import (
	"fmt"

	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// Stage represents one stage in a bgpfix pipe.
// It must represent all config in a koanf instance.
type Stage interface {
	// Index returns the stage index
	Index() int

	// Command returns the stage command
	Command() string

	// Name returns human-friendly name.
	Name() string

	// AddFlags adds stage CLI flags to fs.
	// May return usage string and koanf names for positional args.
	AddFlags(fs *pflag.FlagSet) (usage string, names []string)

	// Init checks config and prepares for Start.
	// TODO: incorporate Is...() output as return value
	Init() error

	// IsReader returns true for pipe directions from which
	// the stage is going to read (not via callbacks).
	IsReader() (L, R bool)

	// IsWriter returns true for pipe directions for which
	// the stage is going to write (not via callbacks).
	IsWriter() (L, R bool)

	// IsRaw returns true if the stage operates on raw BGP I/O,
	// instead (or in addition to) message callbacks.
	IsRaw() bool

	// Start is run as a goroutine after the pipe starts.
	// Returning a non-nil error stops the whole pipe.
	Start() error
}

// NewStageFunc returns a new Stage for name cmd and position idx.
type NewStageFunc func(sb *Base) Stage

// NewStageFuncs maps stage commands to corresponding NewStageFunc
var NewStageFuncs = map[string]NewStageFunc{
	"connect": NewTcpConnect,
	"tcp":     NewTcpConnect,
	"mrt":     NewMrt,
}

// AddStage adds and returns a new stage at idx for cmd,
// or returns the existing instance if it's for the same cmd.
func (b *Bgpipe) AddStage(idx int, cmd string) (Stage, error) {
	// already there? check cmd
	if idx < len(b.Stage) {
		if s := b.Stage[idx]; s != nil {
			if cmd == "" || s.Command() == cmd {
				return s, nil
			} else {
				return nil, fmt.Errorf("[%d] %s: %w: %s", idx, cmd, ErrStageDiff, s.Command())
			}
		}
	}

	// cmd valid?
	newfunc, ok := NewStageFuncs[cmd]
	if !ok {
		return nil, fmt.Errorf("[%d] %s: %w", idx, cmd, ErrStageCmd)
	}

	// prepare for store
	for idx >= len(b.Stage) {
		b.Stage = append(b.Stage, nil)
		b.Koanf = append(b.Koanf, nil)
	}

	// empty config
	k := koanf.New(".")
	b.Koanf[idx] = k

	// create and store
	s := newfunc(NewBase(b, idx, cmd, k))
	b.Stage[idx] = s

	// update b.Last for convenience
	b.Last = len(b.Stage) - 1

	return s, nil
}
