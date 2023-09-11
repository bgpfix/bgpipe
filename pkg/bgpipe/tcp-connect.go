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
	"github.com/knadh/koanf/providers/posflag"
	"github.com/spf13/pflag"
	"golang.org/x/sys/unix"
)

type TcpConnect struct {
	StepBase

	p      *pipe.Pipe
	rhs    bool
	target string
	dialer net.Dialer
}

func NewTcpConnect(b *Bgpipe, cmd string, idx int) Step {
	tc := new(TcpConnect)
	tc.base(b, cmd, idx)
	return tc
}

func (tc *TcpConnect) ParseArgs(args []string) error {
	// setup flags
	f := pflag.NewFlagSet("tcp-connect", pflag.ContinueOnError)
	f.Duration("timeout", 60*time.Second, "connect timeout")
	f.String("md5", "", "TCP MD5 password")

	// parse flags
	if err := f.Parse(args); err != nil {
		return err
	}

	// merge flags into koanf
	tc.k.Load(posflag.Provider(f, ".", tc.k), nil)

	// we need 1 target
	if f.NArg() != 1 {
		return fmt.Errorf("needs 1 argument with the target")
	}
	tc.k.Set("target", f.Arg(0))

	return nil
}

func (tc *TcpConnect) Prepare(p *pipe.Pipe) error {
	// store the pipe
	tc.p = p

	// talking to LHS or RHS?
	switch tc.idx {
	case 0:
		tc.rhs = false // the default case
	case tc.b.Last:
		tc.rhs = true
	default:
		return fmt.Errorf("must be either the first or the last step")
	}

	// prepare the target
	tc.target = tc.k.String("target")
	if len(tc.target) == 0 {
		return fmt.Errorf("no target defined")
	}

	// friendly logger
	id := fmt.Sprintf("[%d] %s", tc.idx, tc.target)
	if tc.rhs {
		id += " (RHS)"
	}
	tc.Logger = tc.b.With().Str("step", id).Logger()

	// has port number?
	_, _, err := net.SplitHostPort(tc.target)
	if err != nil {
		tc.target += ":179" // best-effort try
	}

	// setup TCP MD5?
	if md5pass := tc.k.String("md5"); len(md5pass) > 0 {
		tc.dialer.Control = func(net, _ string, c syscall.RawConn) error {
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

func (tc *TcpConnect) Start() error {
	// derive the context
	timeout := tc.k.Duration("timeout")
	ctx, cancel := context.WithTimeout(tc.b.ctx, timeout)
	defer cancel()

	// connect
	tc.Info().Stringer("timeout", timeout).Msg("connecting")
	conn, err := tc.dialer.DialContext(ctx, "tcp", tc.target)
	if err != nil {
		return fmt.Errorf("could not connect: %w", err)
	}
	tc.Debug().Msg("connected")

	// direction?
	input := tc.p.R
	output := tc.p.L
	if tc.rhs {
		input, output = output, input
	}

	// variables for reader / writer
	var wg sync.WaitGroup
	var rn, wn int64
	var rerr, werr error

	// read from conn
	wg.Add(1)
	go func() {
		rn, rerr = io.Copy(input, conn)
		if rerr != nil {
			conn.Close()
		}
		input.CloseInput()
		wg.Done()
	}()

	// write to conn
	wg.Add(1)
	go func() {
		wn, werr = io.Copy(conn, output)
		if werr != nil {
			conn.Close()
		}
		input.CloseInput()
		wg.Done()
	}()

	// wait
	wg.Wait()

	// report
	log := tc.With().Int64("read", rn).Int64("wrote", wn).Logger()
	if err := errors.Join(rerr, werr); err != nil {
		log.Error().Err(err).Msg("connection error")
		return err
	} else {
		log.Info().Msg("connection closed")
		return nil
	}
}
