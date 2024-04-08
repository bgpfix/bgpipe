package stages

import (
	"errors"
	"fmt"
	"os"

	"github.com/bgpfix/bgpfix/mrt"
	"github.com/bgpfix/bgpipe/core"
)

type Mrt struct {
	*core.StageBase

	fpath string
	mr    *mrt.Reader
}

func NewMrt(parent *core.StageBase) core.Stage {
	s := &Mrt{StageBase: parent}

	o := &s.Options
	o.IsProducer = true
	o.Descr = "read MRT file with BGP4MP messages (uncompress if needed)"
	o.Args = []string{"path"}

	f := o.Flags
	f.Bool("time", false, "overwrite MRT message time")
	f.Bool("notags", false, "do not set tags using BGP4MP header")

	return s
}

func (s *Mrt) Attach() error {
	k := s.K

	s.fpath = k.String("path")
	if len(s.fpath) == 0 {
		return errors.New("needs source file path")
	}

	_, err := os.Stat(s.fpath)
	if err != nil {
		return err
	}

	// MRT-BGP reader writing to s.Dir
	s.mr = mrt.NewReader(s.Ctx)
	mo := &s.mr.Options
	mo.Logger = &s.Logger
	mo.NoTime = k.Bool("time")
	mo.NoTags = k.Bool("notags")

	// attach as pipe input
	return s.mr.Attach(s.P, s.Dir)
}

func (s *Mrt) Run() error {
	n, err := s.mr.ReadFromPath(s.fpath)
	if err != nil {
		return fmt.Errorf("reading from %s failed: %w", s.fpath, err)
	}

	s.Info().
		Int64("read", n).Interface("stats", &s.mr.Stats).
		Msg("reading finished")
	return nil
}
