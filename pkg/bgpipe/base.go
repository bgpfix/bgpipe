package bgpipe

import (
	"fmt"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

// Base provides a building block for Stage implementations
type Base struct {
	zerolog.Logger

	idx int
	cmd string

	B *Bgpipe
	K *koanf.Koanf
	P *pipe.Pipe
}

func NewBase(b *Bgpipe, idx int, cmd string, k *koanf.Koanf) *Base {
	sb := &Base{
		B:   b,
		idx: idx,
		cmd: cmd,
		K:   k,
		P:   b.Pipe,
	}

	sb.Logger = b.With().Str("stage", sb.Name()).Logger()
	return sb
}

func (sb *Base) SetLogId(id string) {
	sb.Logger = sb.B.Logger.With().Str("stage", id).Logger()
}

func (sb *Base) IsFirst() bool {
	return sb.idx == 1
}

func (sb *Base) IsLast() bool {
	return sb.idx == sb.B.Last
}

func (sb *Base) Input() *pipe.Direction {
	if sb.K.Bool("left") {
		return sb.P.L
	} else {
		return sb.P.R
	}
}

func (sb *Base) Output() *pipe.Direction {
	if sb.K.Bool("left") {
		return sb.P.R
	} else {
		return sb.P.L
	}
}

// -----------

func (sb *Base) Index() int {
	return sb.idx
}

func (sb *Base) Command() string {
	return sb.cmd
}

func (sb *Base) Name() string {
	return fmt.Sprintf("[%d] %s", sb.idx, sb.cmd)
}

func (sb *Base) IsReader() (L, R bool) { return }

func (sb *Base) IsWriter() (L, R bool) { return }

func (sb *Base) IsRaw() bool { return false }

func (sb *Base) AddFlags(fs *pflag.FlagSet) (usage string, names []string) {
	return
}

func (sb *Base) Init() error {
	return nil
}

func (sb *Base) Start() error {
	return nil
}
