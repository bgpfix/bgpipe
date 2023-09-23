package stages

import (
	"errors"
	"fmt"
	"os"

	"github.com/bgpfix/bgpfix/mrt"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Mrt struct {
	*bgpipe.StageBase

	fpath string
	br    *mrt.Reader
}

func NewMrt(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Mrt{StageBase: parent}
	s.Descr = "read MRT file with BGP4MP messages"
	s.Usage = "PATH\nProvides MRT file reader, with uncompression if needed."
	s.Args = []string{"path"}

	s.IsWriter = true
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
	s.br = mrt.NewReader(s.Ctx, &s.Logger, s.Upstream())
	return nil
}

func (s *Mrt) Start() error {
	n, err := s.br.ReadFromPath(s.fpath)
	if err != nil {
		return fmt.Errorf("reading from %s failed: %w", s.fpath, err)
	}

	s.Info().
		Int64("read", n).Interface("stats", &s.br.Stats).
		Msg("reading finished")
	return nil
}
