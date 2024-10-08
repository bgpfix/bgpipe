package stages

import (
	"fmt"
	"net"
	"runtime"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
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

	f.Duration("timeout", 0, "connect timeout (0 means none)")
	f.Duration("closed", time.Second, "half-closed timeout (0 means none)")
	if runtime.GOOS == "linux" {
		f.String("md5", "", "TCP MD5 password")
	}
	o.Args = []string{"addr"}

	o.Descr = "wait for a BGP client to connect over TCP"
	o.IsProducer = true
	o.IsConsumer = true

	return s
}

func (s *Listen) Attach() error {
	// check config
	s.bind = s.K.String("addr")
	if len(s.bind) == 0 {
		s.bind = ":179" // a default
	}

	// bind needs a port number?
	_, _, err := net.SplitHostPort(s.bind)
	if err != nil {
		s.bind += ":179" // best-effort try
	}

	s.in = s.P.AddInput(s.Dir)
	return nil
}

func (s *Listen) Prepare() error {
	// listen
	var lc net.ListenConfig
	lc.Control = tcp_md5(s.K.String("md5"))
	l, err := lc.Listen(s.Ctx, "tcp", s.bind)
	if err != nil {
		return err
	}

	// add a listen timeout?
	if t := s.K.Duration("timeout"); t > 0 {
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

	// don't listen for more
	l.Close()

	// success
	s.conn = conn
	return nil
}

func (s *Listen) Run() error {
	return tcp_handle(s.StageBase, s.conn, s.in, s.K.Duration("closed"))
}
