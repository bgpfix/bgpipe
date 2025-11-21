package stages

import (
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

type Connect struct {
	*core.StageBase
	in *pipe.Input

	target string
	bind   string
	conn   net.Conn
}

func NewConnect(parent *core.StageBase) core.Stage {
	var (
		s = &Connect{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	o.Descr = "connect to a BGP endpoint over TCP"
	o.IsProducer = true
	o.FilterOut = true
	o.IsConsumer = true

	f.Duration("timeout", time.Second*10, "connect timeout (0 means none)")
	f.Bool("retry", false, "retry connection on temporary errors")
	f.Int("retry-max", 0, "maximum number of connection retries (0 means unlimited)")
	f.Duration("closed", time.Second, "half-closed timeout (0 means none)")
	f.String("md5", "", "TCP MD5 password")
	f.String("bind", "", "local address to bind to (IP or IP:port)")
	o.Args = []string{"addr"}

	return s
}

func (s *Connect) Attach() error {
	// check config
	s.target = s.K.String("addr")
	if len(s.target) == 0 {
		return fmt.Errorf("no target address defined")
	}

	// target needs a port number?
	if _, _, err := net.SplitHostPort(s.target); err != nil {
		if a, err := netip.ParseAddr(s.target); err == nil {
			s.target = netip.AddrPortFrom(a, 179).String()
		} else {
			s.target += ":179" // no idea, best-effort try
		}
	}

	// use bind address if defined
	s.bind = s.K.String("bind")
	if len(s.bind) > 0 {
		// bind needs a port number?
		if _, _, err := net.SplitHostPort(s.bind); err != nil {
			if a, err := netip.ParseAddr(s.bind); err == nil {
				s.bind = netip.AddrPortFrom(a, 0).String()
			} else {
				s.bind += ":0" // no idea, best-effort try
			}
		}
	}

	s.in = s.P.AddInput(s.Dir)
	return nil
}

func (s *Connect) Prepare() error {
	// dialer
	var dialer net.Dialer
	dialer.Control = tcp_md5(s.K.String("md5"))

	// bind local address?
	if len(s.bind) > 0 {
		laddr, err := net.ResolveTCPAddr("tcp", s.bind)
		if err != nil {
			return err
		}
		dialer.LocalAddr = laddr
	}

	// dial with optional retry
	conn, err := dial_retry(s.StageBase, &dialer, "tcp", s.target)
	if err != nil {
		return err
	}
	s.conn = conn

	// publish connection details
	conn_publish(s.StageBase, conn)

	return nil
}

func (s *Connect) Run() error {
	return conn_handle(s.StageBase, s.conn, s.in, s.K.Duration("closed"))
}
