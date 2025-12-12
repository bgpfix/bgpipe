package stages

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
	"github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/zstd"
)

type Read struct {
	*core.StageBase
	eio *extio.Extio

	fpath string   // path argument
	url   *url.URL // if reading from URL

	opt_decompress string

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
	f.String("decompress", "auto", "decompress the output (bzip2/gz/zstd/none/auto)")

	s.eio = extio.NewExtio(parent, extio.MODE_READ, true)
	return s
}

func (s *Read) Attach() error {
	k := s.K

	s.fpath = k.String("path")
	if len(s.fpath) == 0 {
		return errors.New("path must be set")
	}

	// looks like a URL?
	if strings.Contains(s.fpath, "://") {
		v, err := url.Parse(s.fpath)
		if err != nil {
			return fmt.Errorf("invalid URL '%s': %w", s.fpath, err)
		} else if v.Scheme != "http" && v.Scheme != "https" {
			return fmt.Errorf("invalid URL scheme '%s', must be http or https", v.Scheme)
		}
		s.url = v
		s.fpath = v.Path
	} else {
		s.fpath = path.Clean(s.fpath)
	}

	// need to decompress?
	switch strings.ToLower(k.String("decompress")) {
	case "none", "", "false":
		break // no decompression
	case "gz", "gzip":
		s.opt_decompress = ".gz"
	case "zstd", "zst", "zstandard":
		s.opt_decompress = ".zstd"
	case "bzip2", "bzip", "bz2", "bz":
		s.opt_decompress = ".bz2"
	case "auto":
		switch path.Ext(s.fpath) {
		case ".bz2":
			s.opt_decompress = ".bz2"
		case ".gz":
			s.opt_decompress = ".gz"
		case ".zstd", ".zst":
			s.opt_decompress = ".zstd"
		}
	default:
		return fmt.Errorf("--decompress '%s': invalid value", k.String("decompress"))
	}

	return s.eio.Attach()
}

func (s *Read) Prepare() error {
	if s.url != nil {
		s.Info().Msgf("streaming %s", s.url.String())
		resp, err := http.Get(s.url.String())
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("%s: %s", s.url.String(), resp.Status)
		}
		s.fh = resp.Body
	} else {
		s.Info().Msgf("opening file %s", s.fpath)
		fh, err := os.Open(s.fpath)
		if err != nil {
			return err
		}
		s.fh = fh
	}

	// need to uncompress on the fly?
	switch s.opt_decompress {
	case ".bz2":
		r, err := bzip2.NewReader(s.fh, nil)
		if err != nil {
			s.fh.Close()
			return err
		}
		s.rd = r
		s.close = func() { r.Close() }
	case ".gz":
		r, err := gzip.NewReader(s.fh)
		if err != nil {
			s.fh.Close()
			return err
		}
		s.rd = r
		s.close = func() { r.Close() }
	case ".zstd":
		r, err := zstd.NewReader(s.fh)
		if err != nil {
			s.fh.Close()
			return err
		}
		s.rd = r
		s.close = r.Close
	default:
		s.rd = s.fh // no decompression, just use the file handle
	}

	// need to detect data format?
	if s.eio.DetectNeeded() {
		// try from path, then from sample iff needed
		if !s.eio.DetectPath(s.fpath) {
			br := bufio.NewReader(s.rd)
			if !s.eio.DetectSample(br) {
				return fmt.Errorf("could not detect data format")
			}
			s.rd = br // NB: use buffered data
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
