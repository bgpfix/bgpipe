package stages

import (
	"math"
	"os"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
)

type Stdout struct {
	*core.StageBase
	out *os.File
	eio *extio.Extio
}

func NewStdout(parent *core.StageBase) core.Stage {
	s := &Stdout{StageBase: parent}

	o := &s.Options
	o.Descr = "print messages to stdout or file"
	o.IsStdout = true
	o.Bidir = true

	f := s.Options.Flags
	s.eio = extio.NewExtio(parent)
	f.Lookup("copy").Hidden = true
	f.Lookup("write").Hidden = true
	f.Lookup("read").Hidden = true
	f.Lookup("seq").Hidden = true
	f.Lookup("time").Hidden = true
	f.String("file", "", "append to given file instead of stdout")

	return s
}
func (s *Stdout) Prepare() error {
	if v := s.K.String("file"); len(v) > 0 {
		fh, err := os.OpenFile(v, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		s.out = fh
	} else {
		s.out = os.Stdout
	}
	return nil
}

func (s *Stdout) Attach() error {
	// attach stdout first
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
	eio := s.eio
	for bb := range eio.Output {
		_, err = bb.WriteTo(s.out)
		if err != nil {
			break
		}
		eio.Put(bb)
	}
	eio.OutputClose()
	return err
}

func (s *Stdout) Stop() error {
	if s.out != os.Stdout {
		return s.out.Close()
	}
	s.eio.OutputClose()
	return nil
}
