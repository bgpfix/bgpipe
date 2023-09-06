package bgpipe

import (
	"fmt"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
)

// Step represents one step in a bgpfix pipe.
// It must represent all config in a koanf instance.
type Step interface {
	// Name returns human-friendly name.
	Name() string

	// GetKoanf returns step koanf.
	GetKoanf() *koanf.Koanf

	// SetKoanf overwrites step koanf.
	SetKoanf(k *koanf.Koanf)

	// ParseArgs parses given CLI args into koanf.
	ParseArgs(args []string) error

	// Prepare checks config and attaches to pipe p.
	Prepare(p *pipe.Pipe) error

	// Start is run as a goroutine after the pipe starts.
	// Returning a non-nil error stops the whole pipe.
	Start() error
}

// NewStepFunc returns a new Step for name cmd and position idx.
type NewStepFunc func(b *Bgpipe, cmd string, idx int) Step

// NewStepFuncs maps step commands to corresponding NewStepFunc
var NewStepFuncs = map[string]NewStepFunc{
	"connect": NewTcpConnect,
}

// StepBase provides a building block for Step implementations
type StepBase struct {
	zerolog.Logger
	b   *Bgpipe
	cmd string
	idx int
	k   *koanf.Koanf
}

func (sb *StepBase) base(b *Bgpipe, cmd string, idx int) {
	sb.b = b
	sb.cmd = cmd
	sb.idx = idx
	sb.k = koanf.New(".")

	sb.Logger = b.With().Str("step", sb.Name()).Logger()
}

func (sb *StepBase) Name() string {
	return fmt.Sprintf("[%d] %s", sb.idx, sb.cmd)
}

func (sb *StepBase) GetKoanf() *koanf.Koanf {
	return sb.k
}

func (sb *StepBase) SetKoanf(k *koanf.Koanf) {
	sb.k = k
}
