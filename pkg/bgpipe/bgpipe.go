package bgpipe

import (
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

type Bgpipe struct {
	Flags *pflag.FlagSet
	Koanf *koanf.Koanf

	Steps []Step
}

var NewStepFunc = map[string]func(*Bgpipe) Step{
	"tcp-connect": NewTcpConnect,
	"connect":     NewTcpConnect,
}

func (b *Bgpipe) Init() error {
	b.Flags = pflag.NewFlagSet("bgpipe", pflag.ExitOnError)
	f := b.Flags
	f.SetInterspersed(false)
	f.String("foo", "bar", "help for foo")

	// parse CLI flags
	if err := f.Parse(os.Args[1:]); err != nil {
		return err
	}

	// parse step by step in CLI args
	args := f.Args()
	steps := 0
	for len(args) > 0 {
		// skip empty steps
		if args[0] == "--" {
			args = args[1:]
			continue
		} else {
			steps++
		}

		// is an IP address (+port)?
		var cmd string
		if _, err := netip.ParseAddrPort(args[0]); err == nil {
			cmd = "tcp-connect"
		} else if _, err := netip.ParseAddr(args[0]); err == nil {
			cmd = "tcp-connect"
		} else {
			cmd = args[0]
			args = args[1:]
		}

		// lookup and create step s
		newfunc, ok := NewStepFunc[cmd]
		if !ok {
			return fmt.Errorf("step %d: invalid command '%s'", steps, cmd)
		}
		s := newfunc(b)

		// find the end of args
		var end int
		var inopt bool
		for end = 1; end < len(args); end++ {
			if args[end] == "--" {
				break
			} else if args[end][0] == '-' {
				inopt = strings.IndexByte(args[end], '=') == -1
			} else if inopt {
				inopt = false
			} else {
				break
			}
		}

		// init
		sargs := append([]string{cmd}, args[:end]...)
		err := s.Init(steps, sargs)
		if err != nil {
			return fmt.Errorf("step %d %s: init failed: %w", steps, sargs, err)
		}

		// next step
		if args[end] == "--" {
			args = args[end+1:]
		} else {
			args = args[end:]
		}
	}

	// convert to koanf for convenience
	b.Koanf = koanf.New(".")
	b.Koanf.Load(posflag.Provider(b.Flags, ".", b.Koanf), nil)
	return nil
}
