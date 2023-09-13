package bgpipe

import (
	"fmt"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
)

// Stage represents one stage in a bgpfix pipe.
// It must represent all config in a koanf instance.
type Stage interface {
	// Name returns human-friendly name.
	Name() string

	// GetKoanf returns stage koanf.
	GetKoanf() *koanf.Koanf

	// SetKoanf overwrites stage koanf.
	SetKoanf(k *koanf.Koanf)

	// ParseArgs parses given CLI args into koanf.
	ParseArgs(args []string) error

	// Prepare checks config and attaches to pipe p.
	Prepare(p *pipe.Pipe) error

	// Start is run as a goroutine after the pipe starts.
	// Returning a non-nil error stops the whole pipe.
	Start() error
}

// NewStageFunc returns a new Stage for name cmd and position idx.
type NewStageFunc func(b *Bgpipe, cmd string, idx int) Stage

// NewStageFuncs maps stage commands to corresponding NewStageFunc
var NewStageFuncs = map[string]NewStageFunc{
	"connect": NewTcpConnect,
	"tcp":     NewTcpConnect,
	"mrt":     NewMrt,
}

// StageBase provides a building block for Stage implementations
type StageBase struct {
	zerolog.Logger
	b   *Bgpipe
	cmd string
	idx int
	k   *koanf.Koanf
}

func (sb *StageBase) base(b *Bgpipe, cmd string, idx int) {
	sb.b = b
	sb.cmd = cmd
	sb.idx = idx
	sb.k = koanf.New(".")

	sb.Logger = b.With().Str("stage", sb.Name()).Logger()
}

func (sb *StageBase) Name() string {
	return fmt.Sprintf("[%d] %s", sb.idx, sb.cmd)
}

func (sb *StageBase) GetKoanf() *koanf.Koanf {
	return sb.k
}

func (sb *StageBase) SetKoanf(k *koanf.Koanf) {
	sb.k = k
}
