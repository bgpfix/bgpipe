package bgpipe

import (
	"fmt"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/spf13/pflag"
)

type TcpConnect struct {
	b *Bgpipe

	flags *pflag.FlagSet
}

func NewTcpConnect(b *Bgpipe) Step {
	return &TcpConnect{b: b}
}

func (tc *TcpConnect) Init(step int, args []string) error {
	tc.flags = pflag.NewFlagSet("tcp-connect", pflag.ExitOnError)
	f := tc.flags
	// f.SetInterspersed(false)
	f.String("addr", "ipv4", "help for addr")

	if err := f.Parse(args[1:]); err != nil {
		return err
	}

	fmt.Printf("tcp-connect %d %s -> %s\n", step, args, f.Args())
	return nil
}

func (tc *TcpConnect) Attach(p *pipe.Pipe) error {
	return nil
}
