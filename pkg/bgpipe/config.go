package bgpipe

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

func (b *Bgpipe) Configure() error {
	// root config
	b.K = koanf.New(".")
	b.Koanf[0] = b.K

	// parse CLI args
	err := b.cfgFromArgs(os.Args[1:])
	if err != nil {
		return fmt.Errorf("could not parse CLI flags: %w", err)
	}

	// TODO
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	// at least one stage defined?
	if len(b.Stage) < 2 {
		return fmt.Errorf("need at least 1 stage")
	}

	// FIXME: analyze the config and decide if OK and a speaker needed

	return nil
}

func (b *Bgpipe) cfgFromArgs(args []string) error {
	// global CLI flags
	f := pflag.NewFlagSet("bgpipe", pflag.ExitOnError)
	f.SetInterspersed(false)
	f.String("out", "both", "stdout output control (both/tx/rx/none)")
	f.String("in", "tx", "stdin input control (tx/rx/none)")
	if err := f.Parse(args); err != nil {
		return err
	}

	// export flags into koanf
	b.K.Load(posflag.Provider(f, ".", b.K), nil)

	// parse stages and their args
	args = f.Args()
	for idx := 1; len(args) > 0; idx++ {
		// skip empty stages
		if args[0] == "--" {
			args = args[1:]
			continue
		}

		// is args[0] a special value, or generic stage command name?
		var cmd string
		switch {
		case IsAddr(args[0]):
			cmd = "tcp"
		case IsFile(args[0]):
			cmd = "mrt" // FIXME: stat -> mrt / exec / json / etc.
		}

		// not a special value? find the end of its args
		var end int
		if cmd == "" {
			cmd = args[0]
			args = args[1:]
			for end = 0; end < len(args); end++ {
				if args[end] == "--" {
					end++
					break
				}
			}
		} else { // some heuristics to find the end:
			var inopt bool
			for end = 1; end < len(args); end++ {
				if args[end] == "--" {
					end++
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

		// get s
		s, err := b.AddStage(idx, cmd)
		if err != nil {
			return err
		}

		// setup flags
		sf := pflag.NewFlagSet(cmd, pflag.ExitOnError)
		sf.SortFlags = false
		sf.BoolP("left", "L", false, "L direction")
		sf.BoolP("right", "R", false, "R direction")
		usage, names := s.AddFlags(sf)

		// override usage
		if len(usage) == 0 {
			usage = strings.ToUpper(strings.Join(names, " "))
		}
		sf.Usage = func() {
			fmt.Fprintf(os.Stderr, "Stage usage: %s %s\n", cmd, usage)
			fmt.Fprint(os.Stderr, sf.FlagUsages())
		}

		// parse stage args
		if err := sf.Parse(args[:end]); err != nil {
			return fmt.Errorf("%s: %w", s.Name(), err)
		}
		sargs := sf.Args()

		// export to koanf
		sk := b.Koanf[idx]
		sk.Load(posflag.Provider(sf, ".", sk), nil)
		sk.Set("args", sargs)
		for i, name := range names {
			if i < len(sargs) {
				sk.Set(name, sargs[i])
			} else {
				break
			}
		}

		// next stage
		args = args[end:]
	}

	return nil
}
