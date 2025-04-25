package stages

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
)

type Pipe struct {
	*core.StageBase
	eio   *extio.Extio
	fpath string
	flag  int
	fh    *os.File
}

func NewPipe(parent *core.StageBase) core.Stage {
	s := &Pipe{StageBase: parent}

	o := &s.Options
	o.IsProducer = true
	o.FilterIn = true
	o.FilterOut = true
	o.Bidir = true
	o.Descr = "process messages through a named pipe"
	o.Args = []string{"path"}

	s.eio = extio.NewExtio(parent, 0)

	return s
}

func (s *Pipe) Attach() error {
	k := s.K

	s.fpath = k.String("path")
	if len(s.fpath) == 0 {
		return errors.New("path must be set")
	}
	s.fpath = filepath.Clean(s.fpath)
	s.flag = os.O_RDWR

	return s.eio.Attach()
}

func (s *Pipe) Prepare() error {
	s.Info().Msgf("opening %s", s.fpath)
	fh, err := os.OpenFile(s.fpath, s.flag, 0666)
	if err != nil {
		return err
	}
	s.fh = fh // closed in .Stop()
	return nil
}

func (s *Pipe) Run() error {
	// start pipe reader
	reader_done := make(chan error, 1)
	go s.pipeReader(reader_done)

	// start pipe writer
	writer_done := make(chan error, 1)
	go s.pipeWriter(writer_done)

	// wait
	for {
		select {
		case err := <-reader_done:
			s.Debug().Err(err).Msg("pipe reader done")
			return err
		case err := <-writer_done:
			s.Debug().Err(err).Msg("pipe writer done")
			return err
		case <-s.Ctx.Done():
			err := context.Cause(s.Ctx)
			s.Debug().Err(err).Msg("context cancel")
			return err
		}
	}
}

func (s *Pipe) Stop() error {
	s.eio.InputClose()
	s.eio.OutputClose()
	s.fh.Close()
	return nil
}

func (s *Pipe) pipeReader(done chan error) {
	done <- s.eio.ReadStream(s.fh, nil)
	close(done)
}

func (s *Pipe) pipeWriter(done chan error) {
	done <- s.eio.WriteStream(s.fh)
	close(done)
}
