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

	target      string
	transparent bool
	conn        net.Conn
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
	f.Bool("transparent", false, "transparent proxy mode (Linux TPROXY; spoof source, derive endpoints from the listen side)")
	f.Int("ttl", 0, "outgoing IP TTL / hop limit (0 means default)")
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
	s.transparent = s.K.Bool("transparent")
	if len(s.target) == 0 {
		if !s.transparent {
			return fmt.Errorf("no target address defined")
		}
		// transparent: target derived from the pipe KV in Prepare
	} else if _, _, err := net.SplitHostPort(s.target); err != nil {
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
	dialer.Control = util.Chain(util.TcpMd5(k.String("md5")), util.Ttl(k.Int("ttl")))

	// transparent proxy: spoof source and derive endpoints from the pipe KV
	// populated by the inbound (listen) side, unless given explicitly
	if s.transparent {
		dialer.Control = util.Chain(dialer.Control, util.Transparent())

		deriveTarget := targetUnspecified(s.target)
		if (deriveTarget || k.String("bind") == "") && len(k.Strings("wait")) == 0 {
			s.Warn().Msg("transparent: no -W/--wait set, the inbound tuple may not be in the pipe yet")
		}

		if deriveTarget {
			ap, ok := s.kvAddrPort("L_LOCAL")
			if !ok {
				return fmt.Errorf("transparent: no L_LOCAL in pipe (give a target or wait for the listen side)")
			}
			s.target = ap.String()
		}
		if k.String("bind") == "" {
			ap, ok := s.kvAddrPort("L_REMOTE")
			if !ok {
				return fmt.Errorf("transparent: no L_REMOTE in pipe (set --bind or wait for the listen side)")
			}
			dialer.LocalAddr = net.TCPAddrFromAddrPort(ap)
		}
	}

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

// kvAddrPort reads a netip.AddrPort published in the pipe KV store.
func (s *Connect) kvAddrPort(key string) (netip.AddrPort, bool) {
	v, ok := s.P.KV.Load(key)
	if !ok {
		return netip.AddrPort{}, false
	}
	ap, ok := v.(netip.AddrPort)
	return ap, ok
}

// targetUnspecified reports whether target is empty or an unspecified IP
// (eg. 0.0.0.0 / ::), ie. a placeholder asking to derive it from the pipe KV.
func targetUnspecified(target string) bool {
	if target == "" {
		return true
	}
	host := target
	if h, _, err := net.SplitHostPort(target); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsUnspecified()
}
