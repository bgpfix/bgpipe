package stages

import (
	"fmt"
	"net"
	"runtime"
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
	if runtime.GOOS == "linux" {
		f.String("md5", "", "TCP MD5 password")
	}
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
	if v := k.String("md5"); v != "" {
		lc.Control = util.TcpMd5(v)
	}
	if v := k.Duration("keepalive"); v != 0 {
		lc.KeepAlive = v
	}

	// start listening
	l, err := lc.Listen(s.Ctx, "tcp", s.bind)
	if err != nil {
		return err
	}

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

	// don't listen for any more connections
	l.Close()

	// publish connection details
	util.ConnPublish(s.StageBase, conn)

	// success
	return nil
}

func (s *Listen) Run() error {
	return util.ConnBGP(s.StageBase, s.conn, s.in)
}
