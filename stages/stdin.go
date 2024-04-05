package stages

import (
	"bufio"
	"os"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
)

type Stdin struct {
	*core.StageBase
	in  *os.File
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
	f.String("file", "", "read given file instead of stdin")

	return s
}

func (s *Stdin) Prepare() error {
	if v := s.K.String("file"); len(v) > 0 {
		fh, err := os.Open(v)
		if err != nil {
			return err
		}
		s.in = fh
	} else {
		s.in = os.Stdin
	}
	return nil
}

func (s *Stdin) Attach() error {
	s.K.Set("read", true)
	return s.eio.Attach()
}

func (s *Stdin) Run() error {
	stdin := bufio.NewScanner(s.in) // TODO: bigger buffer than 64KiB?
	for stdin.Scan() {
		err := s.eio.ReadInput(stdin.Bytes(), nil)
		if err != nil {
			return err
		}
	}
	return stdin.Err()
}

func (s *Stdin) Stop() error {
	if s.in != os.Stdin {
		return s.in.Close()
	}
	s.eio.InputClose()
	return nil
}
