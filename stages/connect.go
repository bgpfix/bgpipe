package stages

import (
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/util"
)

type Connect struct {
	*core.StageBase
	in *pipe.Input

	target string
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

	o.Args = []string{"addr"}
	f.String("bind", "", "local address to bind to (IP or IP:port)")
	f.String("md5", "", "TCP MD5 password")
	f.Duration("timeout", 15*time.Second, "TCP connect timeout (0 means off)")
	f.Duration("closed-timeout", time.Second, "TCP half-closed timeout (0 means off)")
	f.Duration("keepalive", 15*time.Second, "TCP keepalive period (-1 means off)")
	f.Bool("retry", false, "retry connection on temporary errors")
	f.Int("retry-max", 0, "maximum number of connection retries (0 means unlimited)")
	f.Bool("tls", false, "connect over TLS")
	f.Bool("insecure", false, "do not validate TLS certificates")
	f.Bool("no-ipv6", false, "avoid IPv6 if possible")

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

	s.in = s.P.AddInput(s.Dir)
	return nil
}

func (s *Connect) Prepare() error {
	k := s.K

	// dialer
	var dialer net.Dialer
	dialer.Control = util.TcpMd5(k.String("md5"))

	// dial with optional retry
	conn, err := util.DialRetry(s.StageBase, &dialer, "tcp", s.target)
	if err != nil {
		return err
	}
	s.conn = conn

	// publish connection details
	util.ConnPublish(s.StageBase, conn)

	return nil
}

func (s *Connect) Run() error {
	return util.ConnBGP(s.StageBase, s.conn, s.in)
}
