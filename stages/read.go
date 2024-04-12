package stages

import (
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
)

type Read struct {
	*core.StageBase
	eio   *extio.Extio
	fpath string
	fh    *os.File
	rd    io.Reader
}

func NewRead(parent *core.StageBase) core.Stage {
	s := &Read{StageBase: parent}

	o := &s.Options
	o.IsProducer = true
	o.Bidir = true
	o.Descr = "read messages from file"
	o.Args = []string{"path"}

	f := o.Flags
	f.Bool("uncompress", true, "uncompress based on file extension (.gz/.bz2)")

	s.eio = extio.NewExtio(parent, 1)
	return s
}

func (s *Read) Attach() error {
	k := s.K

	s.fpath = k.String("path")
	if len(s.fpath) == 0 {
		return errors.New("path must be set")
	}
	s.fpath = filepath.Clean(s.fpath)

	k.Set("read", true)
	return s.eio.Attach()
}

func (s *Read) Prepare() error {
	s.Info().Msgf("opening %s", s.fpath)
	fh, err := os.Open(s.fpath)
	if err != nil {
		return err
	}
	s.fh = fh // closed in .Stop()

	// transparent uncompress?
	s.rd = fh
	if s.K.Bool("uncompress") {
		switch filepath.Ext(s.fpath) {
		case ".bz2":
			s.rd = bzip2.NewReader(fh)
		case ".gz":
			s.rd, err = gzip.NewReader(fh)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Read) Run() error {
	return s.eio.ReadStream(s.rd, nil)
}

func (s *Read) Stop() error {
	s.eio.InputClose()
	s.fh.Close()
	return nil
}
