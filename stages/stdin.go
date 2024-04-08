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
	o.Descr = "read messages from stdin"
	o.IsStdin = true
	o.IsProducer = true
	o.Bidir = true

	f := s.Options.Flags
	s.eio = extio.NewExtio(parent)
	f.Lookup("copy").Hidden = true
	f.Lookup("write").Hidden = true
	f.Lookup("read").Hidden = true

	return s
}

func (s *Stdin) Attach() error {
	s.K.Set("read", true)
	return s.eio.Attach()
}

func (s *Stdin) Run() error {
	stdin := bufio.NewScanner(os.Stdin)
	stdin.Buffer(nil, 1024*1024)
	for s.Ctx.Err() == nil && stdin.Scan() {
		err := s.eio.ReadInput(stdin.Bytes(), nil)
		if err != nil {
			return err
		}
	}
	return stdin.Err()
}

func (s *Stdin) Stop() error {
	s.eio.InputClose()
	return nil
}
