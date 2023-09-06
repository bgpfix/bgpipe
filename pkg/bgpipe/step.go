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
	// Name returns step command name.
	Name() string

	// GetKoanf returns step koanf.
	GetKoanf() *koanf.Koanf

	// SetKoanf overwrites step koanf.
	SetKoanf(k *koanf.Koanf)

	// ParseArgs parses given CLI args into koanf.
	ParseArgs(args []string) error

	// Attach prepares to work in given bgpfix pipe p.
	Attach(p *pipe.Pipe) error

	// Run is started after the pipe starts.
	Run() error
}

// NewStepFunc returns a new Step for name cmd and position pos.
type NewStepFunc func(b *Bgpipe, cmd string, pos int) Step

// NewStepFuncs maps step commands to corresponding NewStepFunc
var NewStepFuncs = map[string]NewStepFunc{
	"connect": NewTcpConnect,
}

// StepBase provides a building block for Step implementations
type StepBase struct {
	zerolog.Logger
	b   *Bgpipe
	cmd string
	pos int
	k   *koanf.Koanf
}

func (sb *StepBase) base(b *Bgpipe, cmd string, pos int) {
	sb.b = b
	sb.cmd = cmd
	sb.pos = pos
	sb.k = koanf.New(".")

	stepname := fmt.Sprintf("%s[%d]", cmd, pos)
	sb.Logger = b.With().Str("step", stepname).Logger()
}

func (sb *StepBase) Name() string {
	return sb.cmd
}

func (sb *StepBase) GetKoanf() *koanf.Koanf {
	return sb.k
}

func (sb *StepBase) SetKoanf(k *koanf.Koanf) {
	sb.k = k
}
