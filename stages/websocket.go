package stages

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	bgpipe "github.com/bgpfix/bgpipe/core"
	"github.com/gorilla/websocket"
)

type Websocket struct {
	*bgpipe.StageBase
	in *pipe.Input

	url  string
	conn *websocket.Conn
}

func NewWebsocket(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s = &Websocket{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	o.Descr = "connect to a JSON endpoint over websocket (HTTPS)"
	o.IsProducer = true
	o.IsConsumer = true

	f.Duration("timeout", time.Second*10, "connect timeout (0 means none)")
	o.Args = []string{"url"}

	return s
}

func (s *Websocket) Attach() error {
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
	s.url = url.String()

	s.in = s.P.AddInput(s.Dir)
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

	// FIXME: close on Stop(), attach to onMsg(), react to incoming messages

	// success
	s.conn = conn
	return nil
}
