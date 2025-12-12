package stages

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
	"github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/zstd"
)

type Write struct {
	*core.StageBase
	eio *extio.Extio

	fpath string
	flags int

	opt_every    time.Duration
	opt_timefmt  string
	opt_compress string

	fh *os.File       // current file handle
	wr io.WriteCloser // current writer, can be gzip.Writer
	n  int64          // number of bytes written to the current file
}

var (
	reTimeFmt = regexp.MustCompile(`\$\{([^}]+)\}`)
)

func NewWrite(parent *core.StageBase) core.Stage {
	s := &Write{StageBase: parent}

	o := &s.Options
	o.Bidir = true
	o.Descr = "write messages to file"
	o.FilterIn = true
	o.Args = []string{"path"}

	s.eio = extio.NewExtio(parent, extio.MODE_WRITE|extio.MODE_COPY, true)
	f := s.Options.Flags
	f.Bool("append", false, "append to file if already exists")
	f.Bool("create", false, "file must not already exist")
	f.String("compress", "auto", "compress the output (bzip2/gz/zstd/none/auto)")
	f.Duration("every", 0, "start new file every time interval")
	f.String("time-format", "20060102.1504", "time format to replace $TIME in paths")
	return s
}

func (s *Write) Attach() error {
	k := s.K

	s.fpath = k.String("path")
	if len(s.fpath) == 0 {
		return errors.New("path must be set")
	}
	s.fpath = path.Clean(s.fpath)
	s.flags = os.O_CREATE | os.O_WRONLY

	if k.Bool("append") {
		s.flags |= os.O_APPEND
	} else if k.Bool("create") {
		s.flags |= os.O_EXCL
	} else {
		s.flags |= os.O_TRUNC
	}

	s.opt_every = k.Duration("every")
	if s.opt_every != 0 && s.opt_every < time.Minute {
		return fmt.Errorf("--every must be at least 60s")
	}

	s.opt_timefmt = k.String("time-format")
	if pt := s.pathTime(s.fpath, time.Now()); pt == "" {
		return fmt.Errorf("file path %s: could not resolve time placeholders", s.fpath)
	} else if pt == s.fpath && s.opt_every != 0 {
		return fmt.Errorf("file path %s: $TIME or ${format} must be used with --every", s.fpath)
	}

	switch strings.ToLower(k.String("compress")) {
	case "none", "", "false":
		break // no compression
	case "bzip2", "bzip", "bz2", "bz":
		s.opt_compress = ".bz2"
	case "gz", "gzip":
		s.opt_compress = ".gz"
	case "zstd", "zst", "zstandard":
		s.opt_compress = ".zstd"
	case "auto":
		switch path.Ext(s.fpath) {
		case ".bz2":
			s.opt_compress = ".bz2"
		case ".gz":
			s.opt_compress = ".gz"
		case ".zstd", ".zst":
			s.opt_compress = ".zstd"
		}
	default:
		return fmt.Errorf("--compress '%s': invalid value", k.String("compress"))
	}

	// need to detect data format?
	if s.eio.DetectNeeded() {
		if !s.eio.DetectPath(s.fpath) {
			return fmt.Errorf("could not detect target data format")
		}
	}

	return s.eio.Attach()
}

// pathTime replaces all time format placeholders with formatted time t.
// Returns empty string on any error.
func (s *Write) pathTime(path string, t time.Time) string {
	// replace $TIME?
	if strings.Contains(s.fpath, `$TIME`) {
		if s.opt_timefmt == "" {
			return ""
		} else if str := t.Format(s.opt_timefmt); str != "" {
			path = strings.ReplaceAll(path, `$TIME`, str)
		} else {
			return ""
		}
	}

	// replace ${format} placeholders
	err := false
	path = reTimeFmt.ReplaceAllStringFunc(path, func(m string) string {
		if str := t.Format(m[2 : len(m)-1]); str != "" {
			return str
		} else {
			err = true // error in time format
			return m   // leave the placeholder
		}
	})

	// if there was any error in time format, return empty string
	if err {
		return ""
	} else {
		return path
	}
}

func (s *Write) Prepare() error {
	return s.openFile()
}

// openFile opens the target file if needed
func (s *Write) openFile() error {
	// already open?
	if s.fh != nil {
		return nil
	} else { // reset state
		s.fh = nil
		s.wr = nil
		s.n = 0
	}

	// get target file path
	fpath := s.fpath + ".tmp"
	if strings.Contains(s.fpath, "$") {
		t := time.Now().UTC()
		if s.opt_every > 0 {
			t = t.Truncate(s.opt_every)
		}
		fpath = s.pathTime(s.fpath, t) + ".tmp"
	}

	// create parent directories if they don't exist
	fdir := path.Dir(fpath)
	if err := os.MkdirAll(fdir, 0755); err != nil {
		return fmt.Errorf("failed to create directories for %s: %w", fpath, err)
	}

	// try to open the new target
	s.Debug().Msgf("%s: opening", fpath)
	fh, err := os.OpenFile(fpath, s.flags, 0644)
	if err != nil {
		return err
	}
	s.fh = fh

	// transparent compress?
	switch s.opt_compress {
	case ".bz2":
		w, err := bzip2.NewWriter(fh, nil)
		if err != nil {
			return fmt.Errorf("failed to create bzip2 writer: %w", err)
		}
		s.wr = w
	case ".gz":
		s.wr = gzip.NewWriter(fh)
	case ".zstd":
		w, err := zstd.NewWriter(fh)
		if err != nil {
			return fmt.Errorf("failed to create zstd writer: %w", err)
		}
		s.wr = w
	default:
		s.wr = fh // no compression
	}

	return nil
}

func (s *Write) closeFile(wr io.WriteCloser, fh *os.File, n int64) {
	if wr == nil || fh == nil {
		return // nothing to close
	}

	fpath := fh.Name()
	target, found := strings.CutSuffix(fpath, ".tmp") // remove the .tmp suffix

	if n == 0 {
		s.Debug().Msgf("%s: removing empty file", fpath)
		os.Remove(fpath)
	} else {
		s.Debug().Msgf("%s: writing %d bytes", target, n)
	}

	wr.Close()
	fh.Close()

	if n != 0 && found {
		os.Rename(fpath, target) // publish the file
	}
}

func (s *Write) Run() error {
	defer func() { s.closeFile(s.wr, s.fh, s.n) }()

	eio := s.eio

	var reload <-chan time.Time
	first_run := true
	if s.opt_every != 0 {
		t := time.Now().Truncate(s.opt_every).Add(s.opt_every)
		reload = time.After(time.Until(t))
	}

	for {
		select {
		case bb, ok := <-eio.Output:
			if !ok {
				return nil // output channel closed
			}

			// open the target file if needed
			if err := s.openFile(); err != nil {
				return err
			}

			// write to file
			n, err := bb.WriteTo(s.wr)
			s.n += n
			if err != nil {
				return err
			}
			eio.Put(bb)

		case <-reload:
			// close the current file
			go s.closeFile(s.wr, s.fh, s.n)

			// signal that we need to open a new file
			s.fh = nil

			// change to periodic ticks from here on?
			if first_run {
				first_run = false
				reload = time.Tick(s.opt_every)
			}

		case <-s.Ctx.Done():
			return context.Cause(s.Ctx)
		}
	}
}

func (s *Write) Stop() error {
	s.eio.OutputClose()
	return nil
}
