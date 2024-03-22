package stages

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	bgpipe "github.com/bgpfix/bgpipe/core"
)

type Stdin struct {
	*bgpipe.StageBase
	inL *pipe.Proc
	inR *pipe.Proc
}

func NewStdin(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Stdin{StageBase: parent}

	o := &s.Options
	o.Descr = "read JSON representation from stdin"
	o.IsStdin = true
	o.IsProducer = true
	o.Bidir = true

	f := o.Flags
	f.Bool("seq", false, "ignore sequence numbers")
	f.Bool("time", false, "ignore message time")

	return s
}

func (s *Stdin) Attach() error {
	s.inL = s.P.AddProc(msg.DIR_L)
	s.inR = s.P.AddProc(msg.DIR_R)
	return nil
}

func (s *Stdin) Run() error {
	var (
		p        = s.P
		opt_seq  = s.K.Bool("seq")
		opt_time = s.K.Bool("time")
		stdin    = bufio.NewScanner(os.Stdin) // TODO: bigger buffer than 64KiB?
		ctx      = s.Ctx
		def      = msg.DIR_R
	)

	// default direction?
	if s.B.StageCount() == 1 {
		def = msg.DIR_L
	}

	for stdin.Scan() {
		buf := bytes.TrimSpace(stdin.Bytes())

		// parse into m
		m := p.GetMsg()
		var err error
		switch {
		case len(buf) == 0 || buf[0] == '#':
			continue

		case buf[0] == '[': // full message
			err = m.FromJSON(buf)

		case buf[0] == '{': // update
			m.Use(msg.UPDATE)
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
			m.Use(msg.KEEPALIVE)
		}

		// fix direction?
		if m.Dir == 0 {
			if s.Dir == 0 {
				m.Dir = def
			} else {
				m.Dir = s.Dir
			}
		}

		// context still valid?
		if ctx.Err() != nil {
			break
		}

		// sail
		if m.Dir == msg.DIR_L {
			s.inL.WriteMsg(m)
		} else {
			s.inR.WriteMsg(m)
		}
	}

	if ctx.Err() != nil {
		return context.Cause(ctx)
	}

	return stdin.Err()
}
