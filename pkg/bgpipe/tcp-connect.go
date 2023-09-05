package bgpipe

import (
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/spf13/pflag"
)

type TcpConnect struct {
	b   *Bgpipe
	pos int

	flags *pflag.FlagSet
}

func NewTcpConnect(b *Bgpipe, pos int) Step {
	return &TcpConnect{
		b:   b,
		pos: pos,
	}
}

func (tc *TcpConnect) Init(args []string) error {
	tc.flags = pflag.NewFlagSet("connect", pflag.ExitOnError)
	f := tc.flags
	f.String("md5", "", "TCP MD5 password")

	if err := f.Parse(args); err != nil {
		return err
	}

	// fmt.Printf("tcp-connect %d %s -> %s\n", tc.pos, args, f.Args())
	return nil
}

func (tc *TcpConnect) Attach(p *pipe.Pipe) error {
	return nil
}
