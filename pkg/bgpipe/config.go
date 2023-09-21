package bgpipe

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/providers/posflag"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

func (b *Bgpipe) Configure() error {
	// parse CLI args
	err := b.cfgFromArgs(os.Args[1:])
	if err != nil {
		return fmt.Errorf("could not parse CLI flags: %w", err)
	}

	// TODO
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	// at least one stage defined?
	if len(b.Stages) == 0 {
		return fmt.Errorf("need at least 1 stage")
	}

	// FIXME: analyze the config and decide if OK and a speaker needed

	return nil
}

func (b *Bgpipe) cfgFromArgs(args []string) error {
	// global CLI flags
	f := pflag.NewFlagSet("bgpipe", pflag.ExitOnError)
	f.SetInterspersed(false)
	f.Bool("perr", false, "silently drop parse error messages")
	f.BoolP("short-asn", "2", false, "use 2-byte ASN numbers")
	f.BoolP("reverse", "R", false, "reverse the pipe direction")
	// f.String("out", "both", "stdout output control (both/tx/rx/none)")
	// f.String("in", "tx", "stdin input control (tx/rx/none)")

	if err := f.Parse(args); err != nil {
		return err
	}

	// export flags into koanf
	b.K.Load(posflag.Provider(f, ".", b.K), nil)

	// parse stages and their args
	args = f.Args()
	for idx := 0; len(args) > 0; idx++ {
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
		s, err := b.NewStage(idx, cmd)
		if err != nil {
			return err
		}

		// parse stage args
		if err := s.ParseArgs(args[:end]); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}

		// next stage
		args = args[end:]
	}

	return nil
}
