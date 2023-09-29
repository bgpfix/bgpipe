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

type TcpConnect struct {
	*bgpipe.StageBase

	target string
	dialer net.Dialer
}

func NewTcpConnect(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &TcpConnect{StageBase: parent}
	s.Options.Descr = "dial remote TCP endpoint"

	s.Options.Flags.Duration("timeout", 60*time.Second, "connect timeout")
	s.Options.Flags.String("md5", "", "TCP MD5 password")
	s.Options.Args = []string{"target"}

	// setup I/O
	s.Options.IsRawReader = true
	s.Options.IsRawWriter = true

	return s
}

func (s *TcpConnect) Attach() error {
	// check config
	s.target = s.K.String("target")
	if len(s.target) == 0 {
		return fmt.Errorf("no target defined")
	}

	// more friendly name?
	if s.Name == "tcp" {
		s.Name += " " + s.target
	}

	// target needs a port number?
	_, _, err := net.SplitHostPort(s.target)
	if err != nil {
		s.target += ":179" // best-effort try
	}

	// setup TCP MD5?
	if md5pass := s.K.String("md5"); len(md5pass) > 0 {
		s.dialer.Control = func(net, _ string, c syscall.RawConn) error {
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

	return nil
}

func (s *TcpConnect) Run() error {
	// derive the context
	timeout := s.K.Duration("timeout")
	ctx, cancel := context.WithTimeout(s.Ctx, timeout)
	defer cancel()

	// connect
	s.Debug().Stringer("timeout", timeout).Msg("connecting")
	at := time.Now()
	conn, err := s.dialer.DialContext(ctx, "tcp", s.target)
	if err != nil {
		return fmt.Errorf("could not connect: %w", err)
	}

	// connected
	msec := time.Since(at).Milliseconds()
	s.Info().Int64("msec", msec).Msg("connected")
	defer conn.Close()

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
		rch <- retval{n, err}
	}()

	// write to conn <- read from s.Output
	go func() {
		n, err := io.Copy(conn, s.Downstream())
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
