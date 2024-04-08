package stages

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
)

type Write struct {
	*core.StageBase
	eio   *extio.Extio
	fpath string
	flag  int
	fh    *os.File
}

func NewWrite(parent *core.StageBase) core.Stage {
	s := &Write{StageBase: parent}

	o := &s.Options
	o.Bidir = true
	o.Descr = "write messages to file"
	o.Args = []string{"path"}

	s.eio = extio.NewExtio(parent)
	f := s.Options.Flags
	f.Lookup("copy").Hidden = true
	f.Lookup("write").Hidden = true
	f.Lookup("read").Hidden = true
	f.Lookup("seq").Hidden = true
	f.Lookup("time").Hidden = true
	f.Bool("append", false, "append to file if already exists")
	f.Bool("create", false, "file must not already exist")
	return s
}

func (s *Write) Attach() error {
	k := s.K

	s.fpath = k.String("path")
	if len(s.fpath) == 0 {
		return errors.New("path must be set")
	}
	s.fpath = filepath.Clean(s.fpath)
	s.flag = os.O_CREATE | os.O_WRONLY

	if k.Bool("append") {
		s.flag |= os.O_APPEND
	} else if k.Bool("create") {
		s.flag |= os.O_EXCL
	} else {
		s.flag |= os.O_TRUNC
	}

	s.K.Set("write", true)
	return s.eio.Attach()
}

func (s *Write) Prepare() error {
	s.Info().Msgf("opening %s", s.fpath)
	fh, err := os.OpenFile(s.fpath, s.flag, 0666)
	if err != nil {
		return err
	}

	s.fh = fh // closed in .Stop()
	return nil
}

func (s *Write) Run() (err error) {
	eio := s.eio
	for bb := range eio.Output {
		_, err = bb.WriteTo(s.fh)
		if err != nil {
			break
		}
		eio.Put(bb)
	}
	return err
}

func (s *Write) Stop() error {
	s.eio.OutputClose()
	s.fh.Close()
	return nil
}
