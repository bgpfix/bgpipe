package stages

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	bgpipe "github.com/bgpfix/bgpipe/core"
	"github.com/gorilla/websocket"
	"github.com/valyala/bytebufferpool"
)

type Websocket struct {
	*bgpipe.StageBase
	inL *pipe.Proc
	inR *pipe.Proc

	copy bool
	url  string

	conn   *websocket.Conn                 // websocket conn
	pool   bytebufferpool.Pool             // for mem re-use
	output chan *bytebufferpool.ByteBuffer // our output to conn
}

func NewWebsocket(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Websocket{StageBase: parent}

	o := &s.Options
	o.Descr = "connect a remote JSON processor over websocket"
	o.IsProducer = true
	o.Bidir = true

	f := o.Flags
	f.Duration("timeout", time.Second*10, "connect timeout (0 means none)")
	f.Bool("copy", false, "copy messages to remote processor (instead of moving)")
	o.Args = []string{"url"}

	return s
}

func (s *Websocket) Attach() error {
	s.copy = s.K.Bool("copy")

	// FIXME FIXME FIXME
	s.Dir = s.Dir.Flip()
	s.IsLeft, s.IsRight = s.IsRight, s.IsLeft
	// FIXME FIXME FIXME

	// check config
	s.url = s.K.String("url")
	if len(s.url) == 0 {
		return fmt.Errorf("no target URL defined")
	}

	// check URL
	url, err := url.Parse(s.url)
	if err != nil {
		return fmt.Errorf("target URL: %w", err)
	}
	switch url.Scheme {
	case "ws", "wss":
		break // ok
	case "http":
		url.Scheme = "ws"
	case "https":
		url.Scheme = "wss"
	case "":
		return fmt.Errorf("target URL: needs 'ws://' or 'wss://' prefix")
	default:
		return fmt.Errorf("target URL: invalid scheme: %s", url.Scheme)
	}
	s.url = url.String()

	// attach to pipe
	s.P.OnMsg(s.pipeMsg, s.Dir)
	s.inL = s.P.AddProc(msg.DIR_L)
	s.inR = s.P.AddProc(msg.DIR_R)

	s.output = make(chan *bytebufferpool.ByteBuffer, 100)
	return nil
}

func (s *Websocket) Prepare() error {
	ctx := s.Ctx

	// add timeout?
	if t := s.K.Duration("timeout"); t > 0 {
		v, fn := context.WithTimeout(ctx, t)
		defer fn()
		ctx = v
	}

	// dialer
	dialer := *websocket.DefaultDialer

	// dial
	s.Debug().Msgf("dialing %s", s.url)
	conn, resp, err := dialer.DialContext(ctx, s.url, nil)
	if err != nil {
		return err
	}
	s.Info().
		Interface("headers", resp.Header).
		Msgf("connected %s -> %s", conn.LocalAddr(), conn.RemoteAddr())

	// success
	s.conn = conn
	return nil
}

func (s *Websocket) Stop() error {
	close_safe(s.output)
	return nil
}

func (s *Websocket) Run() (err error) {
	// start conn reader
	conn_reader_done := make(chan error, 1)
	go s.connReader(conn_reader_done)

	// start conn writer
	conn_writer_done := make(chan error, 1)
	go s.connWriter(conn_writer_done)

	// cleanup on exit
	defer func() {
		conn_err := s.conn.Close()
		s.Err(conn_err).Msg("connection closed")

		// escalate the error?
		if conn_err != nil && err == nil {
			err = conn_err
		}
	}()

	// wait
	for {
		select {
		case err := <-conn_reader_done:
			s.Debug().Err(err).Msg("reader done")
			if err == nil {
				err = io.EOF
			}
			return fmt.Errorf("reader closed: %w", err)
		case err := <-conn_writer_done:
			s.Debug().Err(err).Msg("writer done")
			if err == nil {
				err = io.EOF
			}
			return fmt.Errorf("writer closed: %w", err)
		case <-s.Ctx.Done():
			err := context.Cause(s.Ctx)
			s.Debug().Err(err).Msg("context cancel")
			return err
		}
	}
}

func (s *Websocket) connReader(done chan error) {
	var (
		p   = s.P
		def = msg.DIR_R
	)
	defer close(done)

	for {
		// block on conn read
		mt, buf, err := s.conn.ReadMessage()
		if err != nil {
			done <- err
			return
		}
		if mt != websocket.TextMessage {
			s.Warn().Msgf("read invalid message type: %d", mt)
			continue
		}

		// parse into m
		m := p.Get()
		err = m.FromJSON(buf)
		if err != nil {
			s.Err(err).Bytes("buf", buf).Msg("read parse error")
			p.Put(m)
			continue
		}

		// fix direction?
		if s.Dir != 0 {
			m.Dir = s.Dir
		} else if m.Dir == 0 {
			m.Dir = def
		}

		// sail
		if m.Dir == msg.DIR_L {
			s.inL.WriteMsg(m)
		} else {
			s.inR.WriteMsg(m)
		}
	}
}

func (s *Websocket) connWriter(done chan error) {
	defer func() {
		close_safe(s.output)
		close(done)
	}()

	for bb := range s.output {
		// write
		err := s.conn.WriteMessage(websocket.TextMessage, bb.B)
		s.pool.Put(bb)

		// success?
		if err != nil {
			done <- err
			return
		}
	}
}

func (s *Websocket) pipeMsg(m *msg.Msg) (action pipe.Action) {
	// drop the message after?
	if !s.copy {
		// TODO: if enabled, add borrow if not set already, and keep for later re-use
		action |= pipe.ACTION_DROP
	}

	// get from pool, marshal
	bb := s.pool.Get()
	bb.B = m.ToJSON(bb.B)
	// bb.WriteByte('\n')

	// try writing, don't panic on channel closed
	if !send_safe(s.output, bb) {
		return
	}

	return
}
