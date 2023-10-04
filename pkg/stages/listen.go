package stages

import (
	"fmt"
	"net"
	"time"

	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Listen struct {
	*bgpipe.StageBase

	bind string
}

func NewListen(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s = &Listen{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	f.Duration("timeout", 0, "connect timeout (0 means none)")
	f.String("md5", "", "TCP MD5 password")
	o.Args = []string{"bind"}

	o.Descr = "wait for a TCP client to connect"
	o.Events = map[string]string{
		"connected": "new connection accepted",
	}

	o.IsRawReader = true
	o.IsRawWriter = true

	return s
}

func (s *Listen) Attach() error {
	// check config
	s.bind = s.K.String("bind")
	if len(s.bind) == 0 {
		s.bind = ":179" // a default
	}

	// bind needs a port number?
	_, _, err := net.SplitHostPort(s.bind)
	if err != nil {
		s.bind += ":179" // best-effort try
	}

	return nil
}

func (s *Listen) Run() error {
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

	return tcp_handle(s.StageBase, conn)
}
