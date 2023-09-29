package stages

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Stdin struct {
	*bgpipe.StageBase
	pool sync.Pool
}

func NewStdin(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Stdin{StageBase: parent}
	s.Options.Descr = "read JSON representation from stdin"

	f := s.Options.Flags
	f.Bool("seq", false, "ignore sequence numbers")
	f.Bool("time", false, "ignore message time")

	s.Options.IsStdin = true
	s.Options.IsProducer = true
	return s
}

// TODO: rewrite / cleanup
func (s *Stdin) Run() error {
	var (
		p        = s.P
		opt_seq  = s.K.Bool("seq")
		opt_time = s.K.Bool("time")
		stages   = len(s.B.Stages) - 1
	)

	// TODO: respect the context
	scanner := bufio.NewScanner(os.Stdin)
	for {
		// grab new m
		m := s.NewMsg()

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

		// TODO: exabgp format

		default:
			err = errors.New("invalid input")
		}

		if err != nil {
			s.Error().Err(err).Bytes("input", buf).Msg("parse error")
			continue
		}

		// TODO: fix direction
		if s.Dst() != 0 {
			m.Dst = s.Dst()
		}
		if m.Dst == 0 {
			if s.IsLast || stages == 1 {
				m.Dst = msg.DST_L
			} else {
				m.Dst = msg.DST_R
			}
		}

		// fix type?
		if m.Type == msg.INVALID {
			m.Up(msg.KEEPALIVE)
		}

		// overwrite?
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
