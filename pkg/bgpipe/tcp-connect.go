package bgpipe

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpfix/util"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/spf13/pflag"
	"golang.org/x/sys/unix"
)

type TcpConnect struct {
	StepBase

	p      *pipe.Pipe
	target string
	dialer net.Dialer
	conn   net.Conn
}

func NewTcpConnect(b *Bgpipe, cmd string, pos int) Step {
	tc := new(TcpConnect)
	tc.base(b, cmd, pos)
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

	fmt.Printf("%s %d %s:\n%s\n", tc.cmd, tc.pos, args, tc.k.Sprint())
	return nil
}

func (tc *TcpConnect) Attach(p *pipe.Pipe) error {
	// store pipe
	tc.p = p

	// prepare the target
	tc.target = tc.k.String("target")
	if len(tc.target) == 0 {
		return fmt.Errorf("no target defined")
	}

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

func (tc *TcpConnect) Run() (err error) {
	// derive the context
	timeout := tc.k.Duration("timeout")
	ctx, cancel := context.WithTimeout(tc.b.ctx, timeout)
	defer cancel()

	// connect
	tc.Info().Stringer("timeout", timeout).Msgf("dialing %s", tc.target)
	tc.conn, err = tc.dialer.DialContext(ctx, "tcp", tc.target)
	if err != nil {
		return fmt.Errorf("could not dial the target: %w", err)
	}
	tc.Debug().Msg("connected")

	// FIXME
	util.CopyThrough(tc.p, tc.conn, nil)

	return nil
}
