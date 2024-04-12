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

	fh      *os.File
	wr      io.WriteCloser
	timeout time.Time
}

func NewWrite(parent *core.StageBase) core.Stage {
	s := &Write{StageBase: parent}

	o := &s.Options
	o.Bidir = true
	o.Descr = "write messages to file"
	o.Args = []string{"path"}

	s.eio = extio.NewExtio(parent, 2)
	f := s.Options.Flags
	f.Lookup("copy").Hidden = true
	f.Bool("append", false, "append to file if already exists")
	f.Bool("create", false, "file must not already exist")
	f.Bool("compress", true, "compress based on file extension (.gz only)")
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
	} else if s.opt_every != 0 {
		return fmt.Errorf("--every requires the file path to specify $TIME")
	}

	if k.Bool("compress") {
		switch filepath.Ext(s.fpath) {
		case ".bz2":
			return fmt.Errorf("--compress does not support bzip2")
		case ".gz":
			s.opt_compress = ".gz"
		}
	}

	k.Set("copy", true)
	return s.eio.Attach()
}

func (s *Write) Prepare() error {
	return s.reopenFile(time.Now())
}

// reopenFile opens the target file; it can be called repeatedly to update
// the target file path, and re-open the current target file when needed
func (s *Write) reopenFile(now time.Time) error {
	// have some file already opened?
	if s.fh != nil {
		// still good?
		if s.timeout.IsZero() || now.Before(s.timeout) {
			return nil
		}

		// close the current file in background
		go func(wr io.WriteCloser, fh *os.File) {
			s.Debug().Msgf("closing %s", fh.Name())
			wr.Close()
			fh.Close()
		}(s.wr, s.fh)
	}

	// replace $TIME in target
	target := s.fpath
	if s.opt_timefmt != "" {
		t := now
		if s.opt_every > 0 {
			t = t.Truncate(s.opt_every)
			s.timeout = t.Add(s.opt_every)
		}
		target = strings.Replace(target, `$TIME`, t.UTC().Format(s.opt_timefmt), 1)
	}

	// try to open the new target
	s.Info().Msgf("opening %s", target)
	fh, err := os.OpenFile(target, s.flags, 0666)
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

func (s *Write) Run() (err error) {
	defer func() {
		s.Debug().Msgf("closing %s", s.fh.Name())
		s.wr.Close()
		s.fh.Close()
	}()

	eio := s.eio
	last := time.Now()
	for bb := range eio.Output {
		// update the target file first?
		if s.opt_every != 0 && time.Since(last) > time.Second {
			last = time.Now()
			err = s.reopenFile(last)
			if err != nil {
				break
			}
		}

		// write to file
		_, err = bb.WriteTo(s.wr)
		if err != nil {
			break
		}
		eio.Put(bb)
	}
	return err
}

func (s *Write) Stop() error {
	s.eio.OutputClose()
	return nil
}
