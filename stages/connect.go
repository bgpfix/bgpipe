package stages

import (
	"context"
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

	f.Duration("timeout", time.Minute, "connect timeout (0 means none)")
	f.Duration("closed", time.Second, "half-closed timeout (0 means none)")
	f.String("md5", "", "TCP MD5 password")
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
	_, _, err := net.SplitHostPort(s.target)
	if err != nil {
		// a literal IP address?
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
	ctx := s.Ctx

	// add timeout?
	if t := s.K.Duration("timeout"); t > 0 {
		v, fn := context.WithTimeout(ctx, t)
		defer fn()
		ctx = v
	}

	// dialer
	var dialer net.Dialer
	dialer.Control = tcp_md5(s.K.String("md5"))

	// dial
	s.Info().Msgf("dialing %s", s.target)
	conn, err := dialer.DialContext(ctx, "tcp", s.target)
	if err != nil {
		return err
	}
	s.conn = conn

	// publish the IP
	local, remote := conn.LocalAddr(), conn.RemoteAddr()
	if s.IsFirst {
		s.P.KV.Store("L_LOCAL", local.String())
		s.P.KV.Store("L_REMOTE", remote.String())
	} else if s.IsLast {
		s.P.KV.Store("R_LOCAL", local.String())
		s.P.KV.Store("R_REMOTE", remote.String())
	} else {
		s.Error().Msg("stage is neither first nor last")
	}

	return nil
}

func (s *Connect) Run() error {
	return tcp_handle(s.StageBase, s.conn, s.in, s.K.Duration("closed"))
}
