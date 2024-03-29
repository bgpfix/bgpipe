package stages

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
	"github.com/gorilla/websocket"
)

type Websocket struct {
	*core.StageBase

	url        url.URL              // URL adress
	srv        *http.Server         // http server (may be nil)
	clientConn *websocket.Conn      // websocket client conn
	serverConn chan *websocket.Conn // websocket server conns

	eio *extio.Extio
}

func NewWebsocket(parent *core.StageBase) core.Stage {
	s := &Websocket{StageBase: parent}

	o := &s.Options
	o.Descr = "filter JSON messages over websocket"
	o.IsProducer = true
	o.Bidir = true

	f := o.Flags
	f.Duration("timeout", time.Second*10, "connect timeout (0 means none)")
	f.Bool("listen", false, "listen on given URL instead of dialing it")
	// f.String("auth", "", "use HTTP basic auth (user:pass, $ENV_VARIABLE, or /absolute/path)") // TODO
	o.Args = []string{"url"}

	s.eio = extio.NewExtio(parent)
	return s
}

func (s *Websocket) Attach() error {
	// check URL
	url, err := url.Parse(s.K.String("url"))
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
		return fmt.Errorf("target URL: needs 'ws://' or 'wss://' scheme prefix")
	default:
		return fmt.Errorf("target URL: invalid scheme: %s", url.Scheme)
	}
	if url.Path == "" {
		url.Path = "/"
	}
	s.url = *url
	s.serverConn = make(chan *websocket.Conn, 10)

	return s.eio.Attach()
}

func (s *Websocket) Prepare() error {
	if s.K.Bool("listen") {
		return s.prepareServer()
	} else {
		return s.prepareClient()
	}
}

func (s *Websocket) prepareClient() error {
	// websocket dialer
	dialer := *websocket.DefaultDialer
	if t := s.K.Duration("timeout"); t > 0 {
		dialer.HandshakeTimeout = t
	}

	// dial
	url := s.url.String()
	s.Info().Msgf("dialing %s", url)
	conn, resp, err := dialer.DialContext(s.Ctx, url, nil)
	if err != nil {
		return err
	}
	s.Info().
		Interface("headers", resp.Header).
		Msgf("connected %s -> %s", conn.LocalAddr(), conn.RemoteAddr())

	// success
	s.clientConn = conn
	return nil
}

func (s *Websocket) prepareServer() error {
	// prepare mux
	mux := http.NewServeMux()
	mux.HandleFunc(s.url.Path, s.serverHandle)

	// prepare listener
	s.srv = &http.Server{
		Handler:     mux,
		Addr:        s.url.Host,
		BaseContext: func(l net.Listener) context.Context { return s.Ctx },
		// TODO: ErrorLog?
	}

	// ok go!
	s.Info().Msgf("listening on %s", s.url.String())
	go s.serverListen()

	// great success
	return nil
}

func (s *Websocket) serverListen() {
	err := s.srv.ListenAndServe()
	if err != http.ErrServerClosed {
		s.Cancel(fmt.Errorf("listen error: %w", err))
	}
}

func (s *Websocket) serverHandle(w http.ResponseWriter, r *http.Request) {
	// websocket upgrader
	upgrader := &websocket.Upgrader{
		HandshakeTimeout: s.K.Duration("timeout"),
	}

	// can upgrade?
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.Warn().Err(err).Msgf("%s: could not upgrade", r.RemoteAddr)
		return
	}

	// publish conn for broadcasts
	s.serverConn <- conn
	s.eio.Output <- nil // a signal value

	// block on conn reader
	err = s.connReader(conn, nil)
	s.Info().Err(err).Msgf("%s: reader finished", r.RemoteAddr)

	// close
	err = conn.Close()
	if err != nil {
		s.Warn().Err(err).Msgf("%s: close error", r.RemoteAddr)
	}
}

func (s *Websocket) Stop() error {
	close_safe(s.eio.Output)
	if s.clientConn != nil {
		s.clientConn.Close()
	}
	if s.srv != nil {
		s.srv.Close()
	}
	s.eio.InputClose()
	s.eio.OutputClose()
	return nil
}

func (s *Websocket) Run() (err error) {
	// conn writer
	conn_writer_done := make(chan error, 1)
	go s.connWriter(conn_writer_done)

	// client conn reader if needed
	conn_reader_done := make(chan error, 1)
	if s.clientConn != nil {
		go s.connReader(s.clientConn, conn_reader_done)
	}

	// wait for signals
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

func (s *Websocket) connReader(conn *websocket.Conn, done chan error) error {
	defer close_safe(done)

	// tag incoming messages with the remote
	remote := conn.RemoteAddr().String()
	cb := func(m *msg.Msg) bool {
		mx := pipe.MsgContext(m)
		mx.SetTag("websocket-remote", remote)
		return true
	}

	// read messages from conn
	for {
		mt, buf, err := conn.ReadMessage()
		if err != nil {
			send_safe(done, err)
			return err
		}
		if mt != websocket.TextMessage {
			s.Warn().Msgf("%s: read invalid message type: %d", conn.RemoteAddr(), mt)
			continue
		}
		err = s.eio.Read(buf, cb)
		if err != nil {
			send_safe(done, err)
			return err
		}
	}
}

func (s *Websocket) connWriter(done chan error) {
	defer func() {
		close_safe(s.eio.Output)
		close_safe(done)
	}()

	// conn -> critical?
	conns := make(map[*websocket.Conn]bool)
	if s.clientConn != nil {
		conns[s.clientConn] = true
	}
	for len(s.serverConn) > 0 {
		conns[<-s.serverConn] = false
	}

	for bb := range s.eio.Output {
		// signal to reload the server conns?
		if bb == nil {
			for len(s.serverConn) > 0 {
				conns[<-s.serverConn] = false
			}
			continue
		}

		// broadcast buf to all conns
		buf := bytes.TrimSpace(bb.B)
		for conn, critical := range conns {
			err := conn.WriteMessage(websocket.TextMessage, buf)
			if err == nil {
				continue
			}

			if critical {
				send_safe(done, err)
				return
			} else {
				s.Warn().Err(err).Msgf("%s: write error", conn.RemoteAddr())
				delete(conns, conn)
			}
		}

		// re-use bb
		s.eio.Put(bb)
	}
}
