package stages

import (
	"os"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
)

type Stdin struct {
	*core.StageBase
	eio *extio.Extio
}

func NewStdin(parent *core.StageBase) core.Stage {
	s := &Stdin{StageBase: parent}

	o := &s.Options
	o.Descr = "read messages from stdin"
	o.IsStdin = true
	o.IsProducer = true
	o.FilterOut = true
	o.Bidir = true

	s.eio = extio.NewExtio(parent, extio.MODE_READ)
	return s
}

func (s *Stdin) Attach() error {
	return s.eio.Attach()
}

func (s *Stdin) Run() error {
	return s.eio.ReadStream(os.Stdin, nil)
}

func (s *Stdin) Stop() error {
	s.eio.InputClose()
	return nil
}
