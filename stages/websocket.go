package stages

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
	"github.com/gorilla/websocket"
)

type Websocket struct {
	*core.StageBase

	timeout time.Duration // --timeout
	tls     *tls.Config   // TLS config (may be nil)
	headers http.Header   // HTTP headers

	url        url.URL              // URL address
	srv        *http.Server         // http server (may be nil)
	clientConn *websocket.Conn      // websocket client conn
	serverConn chan *websocket.Conn // websocket server conns

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
	f.String("auth", "", "use HTTP basic auth ($ENV_VARIABLE or file path with user:pass)")
	f.String("cert", "", "SSL certificate path")
	f.String("key", "", "SSL private key path")
	f.Bool("insecure", false, "do not verify the SSL certificate")
	f.StringSlice("header", []string{}, "HTTP headers to send in client mode")
	f.Duration("timeout", time.Second*10, "connect timeout (0 means none)")
	o.Args = []string{"url"}

	s.eio = extio.NewExtio(parent, 0, false)
	return s
}

func (s *Websocket) Attach() error {
	// options
	k := s.K
	if t := k.Duration("timeout"); t > 0 {
		s.timeout = t
	} else {
		s.timeout = 10 * time.Second
	}

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
		// read credentials from env or file
		var cred []byte
		if v[0] == '$' && len(v) >= 2 {
			cred = []byte(os.Getenv(v[1:]))
		} else {
			fh, err := os.Open(v)
			if err != nil {
				return fmt.Errorf("--auth: %w", err)
			}
			cred = make([]byte, 128)
			n, err := fh.Read(cred)
			if err != nil {
				return fmt.Errorf("--auth: file %s: %w", v, err)
			}
			cred, _, _ = bytes.Cut(cred[:n], []byte{'\n'})
		}

		// sanity check
		if bytes.IndexByte(cred, ':') < 0 {
			return fmt.Errorf("--auth: invalid format, need user:pass")
		}

		// add HTTP header
		auth := "Basic " + base64.StdEncoding.EncodeToString(cred)
		s.headers.Set("Authorization", auth)
	}

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
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: s.timeout,
		TLSClientConfig:  s.tls,
	}

	// dial
	url := s.url.String()
	s.Info().Msgf("dialing %s", url)
	conn, resp, err := dialer.DialContext(s.Ctx, url, s.headers)
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
		// TODO: ErrorLog?
		Handler:     mux,
		Addr:        s.url.Host,
		BaseContext: func(l net.Listener) context.Context { return s.Ctx },
		TLSConfig:   s.tls,
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
		if r.Header.Get("Authorization") != auth {
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
	}
	conn, err := upgrader.Upgrade(w, r, headers)
	if err != nil {
		s.Warn().Err(err).Msgf("%s: could not upgrade", r.RemoteAddr)
		return
	} else {
		s.Info().Msgf("%s: connected", r.RemoteAddr)
	}

	// publish conn for broadcasts + signal to connWriter
	if !send_safe(s.serverConn, conn) || !send_safe(s.eio.Output, nil) {
		s.Warn().Msgf("%s: could not register new connection", r.RemoteAddr)
		return
	}

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
	defer func() {
		conn.Close()
		close_safe(done)
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
			send_safe(done, err)
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
			send_safe(done, err)
			return err
		}
	}
}

func (s *Websocket) connWriter(done chan error) {
	defer func() {
		close_safe(s.serverConn)
		close_safe(s.eio.Output)
		close_safe(done)
	}()

	// a map of connections, the value is true iff the connection is critical
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
			if conn == nil {
				continue
			}
			err := conn.WriteMessage(websocket.BinaryMessage, buf)
			if err != nil {
				if critical {
					send_safe(done, err)
					return
				} else {
					s.Warn().Err(err).Msgf("%s: write error", conn.RemoteAddr())
					conn.Close()
					delete(conns, conn)
				}
			}
		}

		// re-use bb
		s.eio.Put(bb)
	}
}
