package stages

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Stdin struct {
	*bgpipe.StageBase
	pool sync.Pool
}

func NewStdin(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Stdin{StageBase: parent}
	s.Descr = "read JSON representation from stdin"

	f := s.Flags
	f.Bool("seq", false, "ignore sequence numbers")
	f.Bool("time", false, "ignore message time")

	s.IsProducer = true
	return s
}

func (s *Stdin) Prepare() error {
	// TODO: grep /filter
	// for _, t := range s.K.Strings("grep") {
	// }

	// by default, set LR
	if !(s.K.Bool("left") || s.K.Bool("right")) {
		s.IsLeft = true
		s.IsRight = true
	}

	return nil
}

// TODO: move to bgpfix?
func (s *Stdin) Run() error {
	var (
		p        = s.P
		opt_seq  = s.K.Bool("seq")
		opt_time = s.K.Bool("time")
	)

	// TODO: respect the context
	scanner := bufio.NewScanner(os.Stdin)
	for {
		// FIXME: pc should be set later
		m := s.NewMsg()
		pc := pipe.Context(m)
		pc.Reverse = false
		pc.Index = 0

		// read line, trim it
		if !scanner.Scan() {
			break
		}
		buf := bytes.TrimSpace(scanner.Bytes())

		// detect the format
		var err error
		switch {
		case len(buf) == 0 || buf[0] == '#':
			continue

		case buf[0] == '[': // full message
			err = m.FromJSON(buf)

		case buf[0] == '{': // update
			m.Up(msg.UPDATE)
			err = m.Update.FromJSON(buf)

		// case exabgp // TODO

		default:
			err = errors.New("invalid input")
		}

		if err != nil {
			s.Error().Err(err).Bytes("input", buf).Msg("parse error")
			continue
		}

		// overwrite?
		if s.Dst() != 0 {
			m.Dst = s.Dst()
		} else if m.Dst == 0 {
			if s.IsLast {
				m.Dst = msg.DST_L
			} else {
				m.Dst = msg.DST_R
			}
		}
		if opt_seq {
			m.Seq = 0
		}
		if opt_time {
			m.Time = time.Time{}
		}

		// sail
		if m.Dst == msg.DST_L {
			p.L.WriteMsg(m)
		} else {
			p.R.WriteMsg(m)
		}
	}

	return nil
}
