package stages

import (
	"context"
	"fmt"
	"net"

	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Connect struct {
	*bgpipe.StageBase

	target string
}

func NewConnect(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s = &Connect{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	f.Duration("timeout", 0, "connect timeout (0 means none)")
	f.String("md5", "", "TCP MD5 password")
	o.Args = []string{"target"}

	o.Descr = "connect to a TCP endpoint"
	o.Events = map[string]string{
		"connected": "connection established",
	}

	o.IsRawReader = true
	o.IsRawWriter = true

	return s
}

func (s *Connect) Attach() error {
	// check config
	s.target = s.K.String("target")
	if len(s.target) == 0 {
		return fmt.Errorf("no target defined")
	}

	// target needs a port number?
	_, _, err := net.SplitHostPort(s.target)
	if err != nil {
		s.target += ":179" // best-effort try
	}

	return nil
}

func (s *Connect) Run() error {
	ctx := s.Ctx

	// add timeout?
	if t := s.K.Duration("timeout"); t > 0 {
		v, fn := context.WithTimeout(ctx, t)
		defer fn()
		ctx = v
	}

	// connect
	var dialer net.Dialer
	dialer.Control = tcp_md5(s.K.String("md5"))
	conn, err := dialer.DialContext(ctx, "tcp", s.target)
	if err != nil {
		return err
	}

	// connected
	return tcp_handle(s.StageBase, conn)
}
