package stages

import (
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
	"github.com/klauspost/compress/zstd"
)

type Read struct {
	*core.StageBase
	eio   *extio.Extio
	fpath string
	fh    io.ReadCloser // can be *os.File or http.Response.Body
	rd    io.Reader
	close func() // function to close decompressor, if needed
}

func NewRead(parent *core.StageBase) core.Stage {
	s := &Read{StageBase: parent}

	o := &s.Options
	o.IsProducer = true
	o.FilterOut = true
	o.Bidir = true
	o.Descr = "read messages from file or URL"
	o.Args = []string{"path"}

	f := o.Flags
	f.Bool("uncompress", true, "uncompress based on file extension (.gz/.bz2/.zst)")

	s.eio = extio.NewExtio(parent, extio.MODE_READ)
	return s
}

func (s *Read) Attach() error {
	k := s.K

	s.fpath = k.String("path")
	if len(s.fpath) == 0 {
		return errors.New("path must be set")
	}

	return s.eio.Attach()
}

func (s *Read) Prepare() error {
	s.Info().Msgf("opening %s", s.fpath)

	fp := s.fpath
	if strings.HasPrefix(s.fpath, "http://") || strings.HasPrefix(s.fpath, "https://") {
		resp, err := http.Get(s.fpath)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("%s: %s", s.fpath, resp.Status)
		}
		s.fh = resp.Body
		fp = resp.Request.URL.Path
	} else {
		fh, err := os.Open(s.fpath)
		if err != nil {
			return err
		}
		s.fh = fh
	}

	// need to uncompress on the fly?
	s.rd = s.fh
	if s.K.Bool("uncompress") {
		ext := path.Ext(fp)
		switch ext {
		case ".bz2":
			s.rd = bzip2.NewReader(s.fh)
			// bzip2.NewReader does not need Close
		case ".gz":
			r, err := gzip.NewReader(s.fh)
			if err != nil {
				s.fh.Close()
				return err
			}
			s.rd = r
			s.close = func() { r.Close() }
		case ".zst", ".zstd":
			r, err := zstd.NewReader(s.fh)
			if err != nil {
				s.fh.Close()
				return err
			}
			s.rd = r
			s.close = r.Close
		}
	}

	return nil
}

func (s *Read) Run() error {
	return s.eio.ReadStream(s.rd, nil)
}

func (s *Read) Stop() error {
	s.eio.InputClose()
	if s.close != nil {
		s.close()
	}
	if s.fh != nil {
		s.fh.Close()
	}
	return nil
}
