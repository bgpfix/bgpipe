package stages

import (
	"bufio"
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
	o.Descr = "read JSON representation from stdin"
	o.IsStdin = true
	o.IsProducer = true
	o.Bidir = true

	s.eio = extio.NewExtio(parent)
	f := s.Options.Flags
	f.Lookup("no-write").Hidden = true
	f.Lookup("no-read").Hidden = true
	return s
}

func (s *Stdin) Attach() error {
	s.K.Set("no-write", true)
	return s.eio.Attach()
}

func (s *Stdin) Run() error {
	stdin := bufio.NewScanner(os.Stdin) // TODO: bigger buffer than 64KiB?
	for stdin.Scan() {
		err := s.eio.Read(stdin.Bytes(), nil)
		if err != nil {
			return err
		}
	}
	return stdin.Err()
}
