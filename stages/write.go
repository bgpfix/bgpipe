package stages

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
)

type Write struct {
	*core.StageBase
	eio   *extio.Extio
	fpath string
	flags int

	opt_every    time.Duration
	opt_timefmt  string
	opt_compress string

	fh *os.File       // current file handle
	wr io.WriteCloser // current writer, can be gzip.Writer
	n  int64          // number of bytes written to the current file
}

func NewWrite(parent *core.StageBase) core.Stage {
	s := &Write{StageBase: parent}

	o := &s.Options
	o.Bidir = true
	o.Descr = "write messages to file"
	o.FilterIn = true
	o.Args = []string{"path"}

	s.eio = extio.NewExtio(parent, extio.MODE_WRITE|extio.MODE_COPY)
	f := s.Options.Flags
	f.Bool("append", false, "append to file if already exists")
	f.Bool("create", false, "file must not already exist")
	f.Bool("compress", true, "compress based on the file extension (.gz only)")
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
	s.fpath = filepath.Clean(s.fpath)
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

	if strings.Contains(s.fpath, `$TIME`) {
		s.opt_timefmt = k.String("time-format")
		if time.Now().UTC().Format(s.opt_timefmt) == "" {
			return fmt.Errorf("--time-format '%s': invalid time format", s.opt_timefmt)
		}
	} else if s.opt_every != 0 {
		return fmt.Errorf("--every requires the file path to include $TIME")
	}

	if k.Bool("compress") {
		switch filepath.Ext(s.fpath) {
		case ".bz2":
			return fmt.Errorf("--compress does not support bzip2")
		case ".gz":
			s.opt_compress = ".gz"
		}
	}

	return s.eio.Attach()
}

func (s *Write) Prepare() error {
	return s.openFile(time.Now())
}

// openFile opens the target file; it can be called repeatedly to update
// the target file path, and re-open the current target file when needed
func (s *Write) openFile(now time.Time) error {
	// write to a temporary file
	fpath := s.fpath + ".tmp"

	// replace $TIME in fpath
	if s.opt_timefmt != "" {
		t := now.UTC()
		if s.opt_every > 0 {
			t = t.Truncate(s.opt_every)
		}
		fpath = strings.Replace(fpath, `$TIME`, t.Format(s.opt_timefmt), 1)
	}

	// try to open the new target
	s.Debug().Msgf("%s: opening", fpath)
	fh, err := os.OpenFile(fpath, s.flags, 0644)
	if err != nil {
		return err
	}
	s.fh = fh

	// transparent compress?
	s.wr = fh
	switch s.opt_compress {
	case ".gz":
		s.wr = gzip.NewWriter(fh)
	}

	return nil
}

func (s *Write) closeFile(wr io.WriteCloser, fh *os.File, n int64) {
	fpath := s.fh.Name()
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

			// write to current file
			n, err := bb.WriteTo(s.wr)
			s.n += n
			if err != nil {
				return err
			}
			eio.Put(bb)

		case now := <-reload:
			// change to periodic ticks
			if first_run {
				first_run = false
				reload = time.Tick(s.opt_every)
			}

			// close the current file
			go s.closeFile(s.wr, s.fh, s.n)

			// open a new file
			if err := s.openFile(now); err != nil {
				return err
			}
		}
	}
}

func (s *Write) Stop() error {
	s.eio.OutputClose()
	return nil
}
