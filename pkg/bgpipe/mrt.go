package bgpipe

import (
	"errors"
	"fmt"
	"os"

	"github.com/bgpfix/bgpfix/mrt"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/spf13/pflag"
)

type Mrt struct {
	StageBase

	br    *mrt.BgpReader
	fpath string
}

func NewMrt(b *Bgpipe, cmd string, idx int) Stage {
	tc := new(Mrt)
	tc.base(b, cmd, idx)
	return tc
}

func (m *Mrt) ParseArgs(args []string) error {
	// setup and parse flags
	f := pflag.NewFlagSet("mrt", pflag.ContinueOnError)
	if err := f.Parse(args); err != nil {
		return err
	}

	// merge flags into koanf
	m.k.Load(posflag.Provider(f, ".", m.k), nil)

	// rewrite args
	for i, arg := range f.Args() {
		m.Info().Msgf("arg[%d] = %s", i, arg)
		m.k.Set(fmt.Sprintf("arg[%d]", i), arg)
	}

	return nil
}

func (m *Mrt) Prepare(p *pipe.Pipe) error {
	// check the source file
	m.fpath = m.k.String("arg[0]")
	if len(m.fpath) == 0 {
		return errors.New("needs 1 argument with the source file")
	}
	_, err := os.Stat(m.fpath)
	if err != nil {
		return fmt.Errorf("could not stat the source file: %w", err)
	}

	// by default, send to R
	dir := p.R
	if m.idx > 0 && m.idx == m.b.Last {
		dir = p.L
	}

	m.br = mrt.NewBgpReader(m.b.ctx, &m.Logger, dir)
	return nil
}

func (m *Mrt) Start() error {
	n, err := m.br.ReadFromPath(m.fpath)
	if err != nil {
		m.Error().Err(err).Msg("reading failed")
		return err
	}

	m.Info().Int64("read", n).Msg("reading finished")
	return nil
}
