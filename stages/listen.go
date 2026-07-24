package stages

import (
	"fmt"
	"net"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/util"
)

type Listen struct {
	*core.StageBase
	in *pipe.Input

	bind string
	conn net.Conn
}

func NewListen(parent *core.StageBase) core.Stage {
	var (
		s = &Listen{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	o.Descr = "let a BGP client connect over TCP"
	o.IsProducer = true
	o.FilterOut = true
	o.IsConsumer = true

	o.Args = []string{"addr"}
	f.String("md5", "", "TCP MD5 password")
	f.Bool("transparent", false, "transparent proxy mode (Linux TPROXY)")
	f.Int("ttl", 0, "outgoing IP TTL / hop limit (0 means default)")
	f.Duration("timeout", 0, "TCP connect timeout (0 means off)")
	f.Duration("closed-timeout", time.Second, "TCP half-closed timeout (0 means off)")
	f.Duration("keepalive", 15*time.Second, "TCP keepalive period (-1 means off)")

	return s
}

func (s *Listen) Attach() error {
	// check config
	s.bind = s.K.String("addr")
	if len(s.bind) == 0 {
		s.bind = ":179" // a default
	} else if _, _, err := net.SplitHostPort(s.bind); err != nil {
		s.bind += ":179" // best-effort try
	}

	s.in = s.P.AddInput(s.Dir)
	return nil
}

func (s *Listen) Prepare() error {
	k := s.K

	// listen config
	var lc net.ListenConfig
	var transparent util.ControlFunc
	if k.Bool("transparent") {
		transparent = util.Transparent()
	}
	lc.Control = util.Chain(util.TcpMd5(k.String("md5")), transparent, util.Ttl(k.Int("ttl")))
	if v := k.Duration("keepalive"); v != 0 {
		lc.KeepAlive = v
	}

	// start listening
	l, err := lc.Listen(s.Ctx, "tcp", s.bind)
	if err != nil {
		return err
	}
	defer l.Close() // NB: also stop listening after the first connection

	// add a listen timeout?
	if t := k.Duration("timeout"); t > 0 {
		if tl, _ := l.(*net.TCPListener); tl != nil {
			tl.SetDeadline(time.Now().Add(t))
		} else {
			return fmt.Errorf("could not get TCPListen")
		}
	}

	// wait for first connection
	s.Info().Msgf("listening on %s", l.Addr())
	conn, err := l.Accept()
	if err != nil {
		return err
	}
	s.conn = conn

	// publish connection details
	util.ConnPublish(s.StageBase, conn)

	// success
	return nil
}

func (s *Listen) Run() error {
	return util.ConnBGP(s.StageBase, s.conn, s.in)
}
