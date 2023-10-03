package stages

import (
	"net"
	"sync"

	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Listen struct {
	*bgpipe.StageBase

	wg       sync.WaitGroup
	target   string
	listener net.Listener
	conn     net.Conn
}

func NewListen(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Listen{StageBase: parent}

	o := &s.Options
	o.Descr = "accept 1 remote TCP client"
	o.IsRawReader = true
	o.IsRawWriter = true

	f := o.Flags
	f.String("md5", "", "TCP MD5 password")
	f.String("bind", ":179", "bind address")

	return s
}

func (s *Listen) Attach() (err error) {
	if s.Name == "tcp" {
		s.Name += " " + s.K.String("bind")
	}

	var lc net.ListenConfig
	lc.Control = tcp_md5(s.K.String("md5"))
	s.listener, err = lc.Listen(s.Ctx, "tcp", s.K.String("bind"))
	if err != nil {
		return err
	}

	return nil
}

func (s *Listen) Prepare() error {
	s.Info().Msgf("listening on %s", s.listener.Addr())
	conn, err := s.listener.Accept()
	if err != nil {
		return err
	}
	s.conn = conn
	return nil
}

func (s *Listen) Run() error {
	return tcp_handle(s.StageBase, s.conn)
}
