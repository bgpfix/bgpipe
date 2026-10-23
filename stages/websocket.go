package stages

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
	"github.com/bgpfix/bgpipe/pkg/util"
	"github.com/gorilla/websocket"
)

type Websocket struct {
	*core.StageBase

	timeout   time.Duration // --timeout
	retry     bool          // --retry
	retry_max int           // --retry-max
	tls       *tls.Config   // TLS config (may be nil)
	headers   http.Header   // HTTP headers

	url         url.URL              // URL address
	srv         *http.Server         // http server (may be nil)
	client_conn *websocket.Conn      // websocket client conn
	server_conn chan *websocket.Conn // websocket server conns
	dead_conn   chan *websocket.Conn // server conns with a failed writer

	eio *extio.Extio
}

func NewWebsocket(parent *core.StageBase) core.Stage {
	s := &Websocket{StageBase: parent}

	o := &s.Options
	o.Descr = "process messages over websocket"
	o.IsProducer = true
	o.FilterIn = true
	o.FilterOut = true
	o.Bidir = true

	f := o.Flags
	f.Bool("listen", false, "listen on the URL instead of connecting to it")
	f.String("auth", "", "HTTP basic auth user:pass ($ENV_VAR, /file path, or literal)")
	f.Bool("open", false, "allow --listen without --auth (anyone can read AND inject messages)")
	f.String("cert", "", "SSL certificate path")
	f.String("key", "", "SSL private key path")
	f.Bool("insecure", false, "do not verify the SSL certificate")
	f.StringSlice("header", []string{}, "HTTP headers to send in client mode")
	f.Duration("timeout", time.Second*10, "connect timeout (0 means none)")
	f.Bool("retry", false, "retry client connection on temporary errors")
	f.Int("retry-max", 0, "maximum number of retries (0 means unlimited)")
	o.Args = []string{"url"}

	s.eio = extio.NewExtio(parent, 0, false)
	return s
}

func (s *Websocket) Attach() error {
	// options
	k := s.K
	s.timeout = k.Duration("timeout")
	s.retry = k.Bool("retry")
	s.retry_max = k.Int("retry-max")

	// check URL
	url, err := url.Parse(k.String("url"))
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

	// SSL config
	if s.url.Scheme == "wss" {
		s.tls = &tls.Config{}

		// skip SSL cert verify?
		if k.Bool("insecure") {
			s.tls.InsecureSkipVerify = true
		}

		// load certificate and key
		has_certkey := k.String("cert") != "" && k.String("key") != ""
		if has_certkey {
			cert, err := tls.LoadX509KeyPair(k.String("cert"), k.String("key"))
			if err != nil {
				return fmt.Errorf("--cert and --key: %w", err)
			}
			s.tls.Certificates = append(s.tls.Certificates, cert)
		} else if k.Bool("listen") {
			return fmt.Errorf("wss:// server requires --cert and --key")
		}
	}

	// HTTP headers
	s.headers = make(http.Header)
	s.headers.Set("User-Agent", "bgpipe")
	for _, v := range k.Strings("header") {
		key, val, found := strings.Cut(v, ":")
		if !found {
			return fmt.Errorf("--header %s: colon not found", v)
		}
		s.headers.Set(key, val)
	}

	// HTTP auth
	if v := k.String("auth"); len(v) > 0 {
		cred, err := core.ReadSecret(v)
		if err != nil {
			return fmt.Errorf("--auth: %w", err)
		} else if bytes.IndexByte(cred, ':') < 0 {
			return fmt.Errorf("--auth: invalid format, need user:pass")
		}
		s.headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(cred))
	} else if k.Bool("listen") && !k.Bool("open") {
		return fmt.Errorf("--listen requires --auth (or --open to allow anyone)")
	}

	s.server_conn = make(chan *websocket.Conn, 10)
	s.dead_conn = make(chan *websocket.Conn, 100)
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
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: s.timeout,
		TLSClientConfig:  s.tls,
	}

	// dial
	url := s.url.String()
	for try := 0; ; try++ {
		s.Info().Msgf("dialing %s", url)
		conn, resp, err := dialer.DialContext(s.Ctx, url, s.headers)

		// check the result
		if err == nil {
			s.Info().
				Interface("headers", resp.Header).
				Msgf("connected %s -> %s", conn.LocalAddr(), conn.RemoteAddr())
			conn.SetReadLimit(4 * 1024 * 1024) // 4MB max message
			s.client_conn = conn
			return nil // success
		} else if !s.retry || (s.retry_max > 0 && try >= s.retry_max) {
			return err // no (more) retries
		} else if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			// temporary timeout, retry
		} else if s.timeout > 0 && errors.Is(err, context.DeadlineExceeded) {
			// context timeout, retry
		} else {
			return err // non-temporary error
		}

		sec := min(60, try*try) + rand.Intn(try+1)
		s.Warn().Err(err).Msgf("dial failed, retrying in %d seconds", sec)
		select {
		case <-time.After(time.Second * time.Duration(sec)):
		case <-s.Ctx.Done():
			return context.Cause(s.Ctx)
		}
	}
}

func (s *Websocket) prepareServer() error {
	// prepare mux
	mux := http.NewServeMux()
	mux.HandleFunc(s.url.Path, s.serverHandle)

	// prepare listener
	s.srv = &http.Server{
		Handler:           mux,
		Addr:              s.url.Host,
		BaseContext:       func(l net.Listener) context.Context { return s.Ctx },
		TLSConfig:         s.tls,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ok go!
	s.Info().Msgf("listening on %s", s.url.String())
	go s.serverListen()

	// great success
	return nil
}

func (s *Websocket) serverListen() {
	var err error
	if s.url.Scheme == "wss" {
		err = s.srv.ListenAndServeTLS("", "") // will use srv.TLSConfig.Certificates
	} else {
		err = s.srv.ListenAndServe()
	}
	if err != http.ErrServerClosed {
		s.Cancel(fmt.Errorf("listen error: %w", err))
	}
}

func (s *Websocket) serverHandle(w http.ResponseWriter, r *http.Request) {
	headers := s.headers

	// require authorization?
	if auth := headers.Get("Authorization"); len(auth) > 0 {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(auth)) != 1 {
			s.Warn().Msgf("%s: not authorized", r.RemoteAddr)
			w.Header().Set("WWW-Authenticate", `Basic realm="bgpipe"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// avoid sending auth back to client
		headers = headers.Clone()
		headers.Del("Authorization")
	}

	// websocket upgrader
	upgrader := &websocket.Upgrader{
		HandshakeTimeout: s.timeout,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			return strings.EqualFold(u.Host, r.Host)
		},
	}
	conn, err := upgrader.Upgrade(w, r, headers)
	if err != nil {
		s.Warn().Err(err).Msgf("%s: could not upgrade", r.RemoteAddr)
		return
	} else {
		s.Info().Msgf("%s: connected", r.RemoteAddr)
	}

	conn.SetReadLimit(4 * 1024 * 1024) // 4MB max message

	// publish conn for broadcasts + signal to connWriter
	if !util.Send(s.server_conn, conn) || !util.Send(s.eio.Output, nil) {
		s.Warn().Msgf("%s: could not register new connection", r.RemoteAddr)
		return
	}

	// block on conn reader (closes conn on return)
	err = s.connReader(conn, nil)
	s.Info().Err(err).Msgf("%s: reader finished", r.RemoteAddr)
}

func (s *Websocket) Stop() error {
	util.Close(s.eio.Output)
	if s.client_conn != nil {
		s.client_conn.Close()
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
	if s.client_conn != nil {
		go s.connReader(s.client_conn, conn_reader_done)
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
	defer func() {
		conn.Close()
		util.Close(done)
	}()

	// tag incoming messages with the remote addr
	remote := conn.RemoteAddr().String()
	cb := func(m *msg.Msg) bool {
		tags := pipe.UseContext(m).UseTags()
		tags["websocket/remote"] = remote
		return true
	}

	// read messages from conn
	for {
		mt, buf, err := conn.ReadMessage()
		if err != nil {
			util.Send(done, err)
			return err
		}

		switch mt {
		case websocket.BinaryMessage:
			// ok
		case websocket.TextMessage:
			// ok
		default:
			s.Warn().Msgf("%s: read invalid message type: %d", conn.RemoteAddr(), mt)
			continue
		}

		err = s.eio.ReadSingle(buf, cb)
		if err != nil {
			util.Send(done, err)
			return err
		}
	}
}

func (s *Websocket) connWriter(done chan error) {
	// conns maps connections to their send queues (nil for the critical client conn)
	conns := make(map[*websocket.Conn]chan []byte)
	defer func() {
		util.Close(s.server_conn)
		util.Close(s.eio.Output)
		util.Close(done)
		for _, q := range conns {
			if q != nil {
				close(q)
			}
		}
	}()

	if s.client_conn != nil {
		conns[s.client_conn] = nil
	}
	accept := func() {
		for len(s.server_conn) > 0 {
			conn := <-s.server_conn
			q := make(chan []byte, 1000)
			conns[conn] = q
			go s.queueWriter(conn, q)
		}
	}
	accept()

	for bb := range s.eio.Output {
		// signal to reload the server conns?
		if bb == nil {
			accept()
			continue
		}

		// forget server conns whose writer failed
		for len(s.dead_conn) > 0 {
			conn := <-s.dead_conn
			if q := conns[conn]; q != nil {
				close(q)
				delete(conns, conn)
			}
		}

		// broadcast buf to all conns
		buf := bytes.TrimSpace(bb.B)
		var cp []byte // buf copy for queues
		for conn, q := range conns {
			if q == nil { // the critical client conn
				if err := conn.WriteMessage(websocket.BinaryMessage, buf); err != nil {
					util.Send(done, err)
					return
				}
				continue
			}

			// NB: never block the pipeline on (untrusted) server clients
			if cp == nil {
				cp = bytes.Clone(buf)
			}
			select {
			case q <- cp:
			default:
				s.Warn().Msgf("%s: client too slow, disconnecting", conn.RemoteAddr())
				close(q)
				conn.Close()
				delete(conns, conn)
			}
		}

		// re-use bb
		s.eio.Put(bb)
	}
}

// queueWriter writes q to a server client conn, with a write deadline
func (s *Websocket) queueWriter(conn *websocket.Conn, q chan []byte) {
	defer conn.Close()
	for buf := range q {
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.BinaryMessage, buf); err != nil {
			s.Warn().Err(err).Msgf("%s: write error", conn.RemoteAddr())
			select {
			case s.dead_conn <- conn: // NB: connWriter owns the conns map
			default:
			}
			return
		}
	}
}
