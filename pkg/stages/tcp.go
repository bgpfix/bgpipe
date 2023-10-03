package stages

import (
	"context"
	"fmt"
	"io"
	"net"
	"syscall"
	"time"
	"unsafe"

	"github.com/bgpfix/bgpipe/pkg/bgpipe"
	"golang.org/x/sys/unix"
)

type Tcp struct {
	*bgpipe.StageBase

	target string
}

func NewTcp(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s = &Tcp{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	f.Duration("timeout", 60*time.Second, "connect timeout")
	f.String("md5", "", "TCP MD5 password")
	o.Args = []string{"target"}

	o.Descr = "dial remote TCP endpoint"
	o.Events = map[string]string{
		"connected": "connection established",
	}

	o.IsRawReader = true
	o.IsRawWriter = true

	return s
}

func (s *Tcp) Attach() error {
	// check config
	s.target = s.K.String("target")
	if len(s.target) == 0 {
		return fmt.Errorf("no target defined")
	}

	// target needs a port number?
	_, _, err := net.SplitHostPort(s.target)
	if err != nil {
		s.target += ":179" // best-effort try
	}

	return nil
}

func (s *Tcp) Run() error {
	// derive the context
	timeout := s.K.Duration("timeout")
	ctx, cancel := context.WithTimeout(s.Ctx, timeout)
	defer cancel()

	// connect
	var dialer net.Dialer
	dialer.Control = tcp_md5(s.K.String("md5"))
	conn, err := dialer.DialContext(ctx, "tcp", s.target)
	if err != nil {
		return err
	}

	// connected
	return tcp_handle(s.StageBase, conn)
}

func tcp_md5(md5pass string) func(net, addr string, c syscall.RawConn) error {
	if len(md5pass) == 0 {
		return nil
	}

	return func(net, addr string, c syscall.RawConn) error {
		// setup tcp sig
		var key [80]byte
		l := copy(key[:], md5pass)
		sig := unix.TCPMD5Sig{
			Flags:     unix.TCP_MD5SIG_FLAG_PREFIX,
			Prefixlen: 0,
			Keylen:    uint16(l),
			Key:       key,
		}

		// addr family
		switch net {
		case "tcp6", "udp6", "ip6":
			sig.Addr.Family = unix.AF_INET6
		default:
			sig.Addr.Family = unix.AF_INET
		}

		// setsockopt
		var err error
		c.Control(func(fd uintptr) {
			b := *(*[unsafe.Sizeof(sig)]byte)(unsafe.Pointer(&sig))
			err = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_MD5SIG_EXT, string(b[:]))
		})
		return err
	}

}

func tcp_handle(s *bgpipe.StageBase, conn net.Conn) error {
	s.Info().Msgf("connected %s -> %s", conn.LocalAddr(), conn.RemoteAddr())
	s.Event("connected", nil, conn.LocalAddr(), conn.RemoteAddr())
	defer conn.Close()

	// get tcp conn
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return fmt.Errorf("could not get TCPConn")
	}

	// discard data after conn.Close()
	if err := tcp.SetLinger(0); err != nil {
		s.Info().Err(err).Msg("SetLinger failed")
	}

	// variables for reader / writer
	type retval struct {
		n   int64
		err error
	}
	rch := make(chan retval, 1)
	wch := make(chan retval, 1)

	// read from conn -> write to s.Input
	go func() {
		n, err := io.Copy(s.Upstream(), conn)
		s.Trace().Err(err).Msg("connection reader returned")
		tcp.CloseRead()
		rch <- retval{n, err}
	}()

	// write to conn <- read from s.Output
	go func() {
		n, err := tcp.ReadFrom(s.Downstream())
		s.Trace().Err(err).Msg("connection writer returned")
		tcp.CloseWrite()
		wch <- retval{n, err}
	}()

	// wait for error on any side, or both sides EOF
	var read, wrote int64
	running := 2
	for running > 0 {
		select {
		case <-s.Ctx.Done():
			return context.Cause(s.Ctx)
		case r := <-rch:
			read = r.n
			running--
			if r.err != nil && r.err != io.EOF {
				return r.err
			}
		case w := <-wch:
			wrote = w.n
			running--
			if w.err != nil && w.err != io.EOF {
				return w.err
			}
		}
	}

	s.Info().Int64("read", read).Int64("wrote", wrote).Msg("connection closed")
	return nil
}
