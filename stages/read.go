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
	"sync"
	"time"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
	"github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/zstd"
)

type Read struct {
	*core.StageBase
	eio *extio.Extio

	paths []string // all path arguments, read one after another

	fpath string   // current path
	url   *url.URL // if reading from URL

	decompress string

	mu    sync.Mutex    // guards fh and close (Stop runs concurrently with Run)
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
	o.Descr = "read messages from file(s), URL(s), or stdin (-)"
	o.Args = []string{"path"}

	f := o.Flags
	f.String("decompress", "auto", "decompress the output (bzip2/gz/zstd/none/auto)")

	s.eio = extio.NewExtio(parent, extio.MODE_READ, true)
	return s
}

func (s *Read) Attach() error {
	// NB: more paths with -A, read in the order given, all in the same data format
	s.paths = append([]string{s.K.String("path")}, s.K.Strings("args")...)
	if len(s.paths[0]) == 0 {
		return errors.New("path must be set")
	}

	// check all paths early, then leave the first one set up
	for i := len(s.paths) - 1; i >= 0; i-- {
		if err := s.setup(s.paths[i]); err != nil {
			return err
		}
		// NB: a typo in the 100th path should not surface hours into the run
		if i > 0 && s.url == nil && s.fpath != "-" {
			if _, err := os.Stat(s.fpath); err != nil {
				return err
			}
		}
	}
	return s.eio.Attach()
}

// setup parses the path argument p, making it the current path
func (s *Read) setup(p string) error {
	k := s.K

	s.fpath, s.url, s.decompress = p, nil, ""
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
		s.decompress = ".gz"
	case "zstd", "zst", "zstandard":
		s.decompress = ".zstd"
	case "bzip2", "bzip", "bz2", "bz":
		s.decompress = ".bz2"
	case "auto":
		switch path.Ext(s.fpath) {
		case ".bz2":
			s.decompress = ".bz2"
		case ".gz":
			s.decompress = ".gz"
		case ".zstd", ".zst":
			s.decompress = ".zstd"
		}
	default:
		return fmt.Errorf("--decompress '%s': invalid value", k.String("decompress"))
	}

	return nil
}

func (s *Read) Prepare() error {
	return s.open()
}

// open opens the current path, ready to be read from s.rd
func (s *Read) open() error {
	if s.url != nil {
		s.Info().Msgf("streaming %s", s.url.String())
		req, err := http.NewRequestWithContext(s.Ctx, "GET", s.url.String(), nil)
		if err != nil {
			return err
		}
		// NB: the body is a stream (no total timeout), but don't hang on a dead server
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = 30 * time.Second
		resp, err := (&http.Client{Transport: tr}).Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("%s: %s", s.url.String(), resp.Status)
		}
		s.setFile(resp.Body)
	} else if s.fpath == "-" {
		s.Info().Msg("reading stdin")
		s.setFile(io.NopCloser(os.Stdin))
	} else {
		s.Info().Msgf("opening file %s", s.fpath)
		fh, err := os.Open(s.fpath)
		if err != nil {
			return err
		}
		s.setFile(fh)
	}

	// need to uncompress on the fly?
	switch s.decompress {
	case ".bz2":
		r, err := bzip2.NewReader(s.fh, nil)
		if err != nil {
			s.fh.Close()
			return err
		}
		s.rd = r
		s.setClose(func() { r.Close() })
	case ".gz":
		r, err := gzip.NewReader(s.fh)
		if err != nil {
			s.fh.Close()
			return err
		}
		s.rd = r
		s.setClose(func() { r.Close() })
	case ".zstd":
		r, err := zstd.NewReader(s.fh)
		if err != nil {
			s.fh.Close()
			return err
		}
		s.rd = r
		s.setClose(r.Close)
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
	for i, p := range s.paths {
		if i > 0 {
			if s.Ctx.Err() != nil {
				return nil // stopped
			}
			if err := s.setup(p); err != nil {
				return err
			}
			if err := s.open(); err != nil {
				return err
			}
		}

		err := s.eio.ReadStream(s.rd, nil)
		s.closeFile()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Read) Stop() error {
	s.eio.InputClose()
	s.closeFile()
	return nil
}

func (s *Read) setFile(fh io.ReadCloser) {
	s.mu.Lock()
	s.fh = fh
	s.mu.Unlock()
}

func (s *Read) setClose(f func()) {
	s.mu.Lock()
	s.close = f
	s.mu.Unlock()
}

// closeFile closes the current decompressor and file, if any
func (s *Read) closeFile() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.close != nil {
		s.close()
		s.close = nil
	}
	if s.fh != nil {
		s.fh.Close()
		s.fh = nil
	}
}
