package stages

import (
	"bufio"
	"bytes"
	"context"
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

	o := &s.Options
	o.Descr = "read JSON representation from stdin"
	o.IsStdin = true
	o.IsProducer = true
	o.AllowLR = true

	f := o.Flags
	f.Bool("seq", false, "ignore sequence numbers")
	f.Bool("time", false, "ignore message time")

	return s
}

func (s *Stdin) Run() error {
	var (
		p        = s.P
		opt_seq  = s.K.Bool("seq")
		opt_time = s.K.Bool("time")
		stdin    = bufio.NewScanner(os.Stdin)
		ctx      = s.Ctx
		dst      = s.Dst()
		def      = msg.DST_R
	)

	// default direction?
	if s.B.StageCount() < 2 {
		def = msg.DST_L
	}

	for {
		// grab new m
		m := s.NewMsg()

		// read line, trim it
		if !stdin.Scan() {
			break
		}
		buf := bytes.TrimSpace(stdin.Bytes())
		// s.Trace().Msgf("stdin: %s", buf)

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

		default:
			err = errors.New("invalid input")
		}

		if err != nil {
			s.Error().Err(err).Bytes("input", buf).Msg("parse error")
			continue
		}

		// overwrite metadata?
		if opt_seq {
			m.Seq = 0
		}
		if opt_time {
			m.Time = time.Time{}
		}

		// fix type?
		if m.Type == msg.INVALID {
			m.Up(msg.KEEPALIVE)
		}

		// fix direction?
		if dst != 0 {
			m.Dst = dst
		} else if m.Dst == 0 {
			m.Dst = def
		}

		// context still valid?
		if ctx.Err() != nil {
			break
		}

		// sail
		if m.Dst == msg.DST_L {
			p.L.WriteMsg(m)
		} else {
			p.R.WriteMsg(m)
		}
	}

	if ctx.Err() != nil {
		return context.Cause(ctx)
	}

	return stdin.Err()
}
