package stages

import (
	"math"
	"os"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
)

type Stdout struct {
	*core.StageBase
	eio *extio.Extio
}

func NewStdout(parent *core.StageBase) core.Stage {
	s := &Stdout{StageBase: parent}

	o := &s.Options
	o.Descr = "print messages to stdout"
	o.IsStdout = true
	o.FilterIn = true
	o.Bidir = true

	s.eio = extio.NewExtio(parent, extio.MODE_WRITE|extio.MODE_COPY, false)
	return s
}

func (s *Stdout) Attach() error {
	err := s.eio.Attach()
	if err != nil {
		return err
	}

	// auto stdout? always run as very last
	if s.Index == 0 {
		s.eio.Callback.Post = true
		s.eio.Callback.Order = math.MaxInt
	}

	return nil
}

func (s *Stdout) Run() (err error) {
	return s.eio.WriteStream(os.Stdout)
}

func (s *Stdout) Stop() error {
	s.eio.OutputClose()
	return nil
}
