package bgpipe

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

type Bgpipe struct {
	Flags *pflag.FlagSet
	Koanf *koanf.Koanf

	Args  [][]string
	Steps []Step
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

	// parse steps and their args
	args := f.Args()
	for len(args) > 0 {
		// skip empty steps
		if args[0] == "--" {
			args = args[1:]
			continue
		}

		// is args[0] a special value, or command name?
		var cmd string
		switch {
		case IsAddr(args[0]):
			cmd = "connect"
		case IsFile(args[0]):
			cmd = "file" // TODO: stat -> mrt / exec / json / etc.
		}

		// not a special value? find the end of args
		var end int
		if cmd == "" {
			cmd = args[0]
			args = args[1:]
			for end = 0; end < len(args); end++ {
				if args[end] == "--" {
					break
				}
			}
		} else { // some heuristics to find the end:
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
		}

		// lookup cmd
		pos := len(b.Steps)
		newfunc, ok := NewStepFuncs[cmd]
		if !ok {
			return fmt.Errorf("step[%d]: invalid command '%s'", pos, cmd)
		}

		// store
		b.Steps = append(b.Steps, newfunc(b, pos))
		b.Args = append(b.Args, args[:end])

		// next
		args = args[end:]
	}

	// initialize
	for pos, s := range b.Steps {
		err := s.Init(b.Args[pos])
		if err != nil {
			return fmt.Errorf("step[%d]: init failed: %w", pos, err)
		}
	}

	// TODO: export step flags to koanf
	// TODO: config file
	// TODO: env?

	// convert to koanf for convenience
	b.Koanf = koanf.New(".")
	b.Koanf.Load(posflag.Provider(b.Flags, ".", b.Koanf), nil)
	return nil
}
