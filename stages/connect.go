package stages

import (
	"context"
	"fmt"
	"net"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/bgpipe"
)

type Connect struct {
	*bgpipe.StageBase
	in *pipe.Input

	target string
	conn   net.Conn
}

func NewConnect(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s = &Connect{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	f.Duration("timeout", 0, "connect timeout (0 means none)")
	f.String("md5", "", "TCP MD5 password")
	o.Args = []string{"addr"}

	o.Descr = "connect to a TCP endpoint"
	o.IsProducer = true
	o.IsConsumer = true

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
		s.target += ":179" // best-effort try
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
	conn, err := dialer.DialContext(ctx, "tcp", s.target)
	if err != nil {
		return err
	}

	// success
	s.conn = conn
	return nil
}

func (s *Connect) Run() error {
	return tcp_handle(s.StageBase, s.conn, s.in)
}
