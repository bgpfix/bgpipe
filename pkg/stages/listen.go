package stages

import (
	"net"

	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Listen struct {
	*bgpipe.StageBase

	target string
}

func NewListen(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s = &Listen{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	f.String("md5", "", "TCP MD5 password")
	o.Args = []string{"bind"}

	o.Descr = "accept 1 remote TCP client"
	o.Events = map[string]string{
		"connected": "new connection accepted",
	}

	o.IsRawReader = true
	o.IsRawWriter = true

	return s
}

func (s *Listen) Run() error {
	// listen
	var lc net.ListenConfig
	lc.Control = tcp_md5(s.K.String("md5"))
	l, err := lc.Listen(s.Ctx, "tcp", s.K.String("bind"))
	if err != nil {
		return err
	}

	// wait for first connection
	s.Info().Msgf("listening on %s", l.Addr())
	conn, err := l.Accept()
	if err != nil {
		return err
	}

	return tcp_handle(s.StageBase, conn)
}
