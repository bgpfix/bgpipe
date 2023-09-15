package bgpipe

import (
	"errors"
	"fmt"
	"os"

	"github.com/bgpfix/bgpfix/mrt"
	"github.com/spf13/pflag"
)

type Mrt struct {
	Base

	br    *mrt.BgpReader
	fpath string
}

func NewMrt(b *Base) Stage {
	return &Mrt{Base: *b}
}

func (s *Mrt) AddFlags(f *pflag.FlagSet) (usage string, names []string) {
	usage = "PATH\nProvides MRT file reader, with uncompression if needed."
	names = []string{"path"}
	return
}

func (s *Mrt) Init() error {
	// check the source file
	s.fpath = s.K.String("path")
	if len(s.fpath) == 0 {
		return errors.New("needs source file path")
	}
	_, err := os.Stat(s.fpath)
	if err != nil {
		return fmt.Errorf("could not stat the source file: %w", err)
	}

	// MRT-BGP reader writing to s.Input().In
	s.br = mrt.NewBgpReader(s.B.ctx, &s.Logger, s.Input())
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
