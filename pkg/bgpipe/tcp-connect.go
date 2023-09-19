package bgpipe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/bgpfix/bgpfix/pipe"
	"golang.org/x/sys/unix"
)

type TcpConnect struct {
	*Stage

	target string
	dialer net.Dialer
}

func NewTcpConnect(parent *Stage) StageValue {
	s := &TcpConnect{Stage: parent}

	s.Flags.Duration("timeout", 60*time.Second, "connect timeout")
	s.Flags.String("md5", "", "TCP MD5 password")
	s.Argnames = []string{"target"}

	// setup I/O
	s.IsRawReader = true
	s.IsRawWriter = true

	return s
}

func (s *TcpConnect) Prepare() error {
	// check config
	s.target = s.K.String("target")
	if len(s.target) == 0 {
		return fmt.Errorf("no target defined")
	}

	// friendly name
	name := fmt.Sprintf("[%d] tcp %s", s.Idx, s.target)
	if s.IsFirst() {
		name += " (LHS)"
	} else {
		name += " (RHS)"
	}
	s.SetName(name)

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

func (s *TcpConnect) Start() error {
	// derive the context
	timeout := s.K.Duration("timeout")
	ctx, cancel := context.WithTimeout(s.B.ctx, timeout)
	defer cancel()

	// connect
	s.Info().Stringer("timeout", timeout).Msg("connecting")
	conn, err := s.dialer.DialContext(ctx, "tcp", s.target)
	if err != nil {
		return fmt.Errorf("could not connect: %w", err)
	}
	s.Debug().Msg("connected")

	// variables for reader / writer
	var wg sync.WaitGroup
	var rn, wn int64
	var rerr, werr error

	// read from conn -> write to s.Input
	wg.Add(1)
	go func(dir *pipe.Direction) {
		rn, rerr = io.Copy(dir, conn)
		if rerr != nil {
			conn.Close()
		}
		dir.CloseInput()
		wg.Done()
	}(s.Dst())

	// write to conn <- read from s.Output
	wg.Add(1)
	go func(dir *pipe.Direction) {
		wn, werr = io.Copy(conn, dir)
		if werr != nil {
			conn.Close()
		}
		wg.Done()
	}(s.Src())

	// wait and report
	wg.Wait()
	s.Info().Int64("read", rn).Int64("wrote", wn).Msg("connection closed")

	return errors.Join(rerr, werr)
}
