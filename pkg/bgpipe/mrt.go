package bgpipe

import (
	"errors"
	"fmt"
	"os"

	"github.com/bgpfix/bgpfix/mrt"
)

type Mrt struct {
	*Stage

	fpath string
	br    *mrt.BgpReader
}

func NewMrt(parent *Stage) StageValue {
	s := &Mrt{Stage: parent}
	s.Usage = "PATH\nProvides MRT file reader, with uncompression if needed."
	s.Argnames = []string{"path"}
	return s
}

func (s *Mrt) Prepare() error {
	s.fpath = s.K.String("path")
	if len(s.fpath) == 0 {
		return errors.New("needs source file path")
	}

	_, err := os.Stat(s.fpath)
	if err != nil {
		return err
	}

	// MRT-BGP reader writing to s.Input().In
	s.br = mrt.NewBgpReader(s.B.ctx, &s.Logger, s.Input())
	return nil
}

func (s *Mrt) Start() error {
	n, err := s.br.ReadFromPath(s.fpath)
	if err != nil {
		return fmt.Errorf("reading from %s failed: %w", s.fpath, err)
	}

	s.Info().Int64("read", n).Msg("reading finished")
	return nil
}
