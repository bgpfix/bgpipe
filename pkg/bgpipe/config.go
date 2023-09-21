package bgpipe

import (
	"fmt"
	"os"

	"github.com/knadh/koanf/providers/posflag"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

func (b *Bgpipe) Configure() error {
	// parse CLI args
	err := b.ParseArgs(os.Args[1:])
	if err != nil {
		return fmt.Errorf("could not parse CLI flags: %w", err)
	}

	// debugging level
	if ll := b.K.String("log"); len(ll) > 0 {
		lvl, err := zerolog.ParseLevel(ll)
		if err != nil {
			return err
		}
		zerolog.SetGlobalLevel(lvl)
	}

	return nil
}

func (b *Bgpipe) ParseArgs(args []string) error {
	// global CLI flags
	f := pflag.NewFlagSet("bgpipe", pflag.ExitOnError)
	f.SetInterspersed(false)
	f.String("log", "info", "log level (debug / info / warn / error / disabled)")
	f.BoolP("no-parse-error", "N", false, "silently drop parse error messages")
	f.BoolP("short-asn", "2", false, "use 2-byte ASN numbers")
	f.BoolP("reverse", "R", false, "reverse the pipe")
	// f.String("out", "both", "stdout output control (both/tx/rx/none)")
	// f.String("in", "tx", "stdin input control (tx/rx/none)")

	// parse and export into koanf
	if err := f.Parse(args); err != nil {
		return err
	} else {
		b.K.Load(posflag.Provider(f, ".", b.K), nil)
	}

	// parse stages and their args
	args = f.Args()
	for idx := 0; len(args) > 0; idx++ {
		// skip empty stages
		if args[0] == "--" {
			args = args[1:]
			continue
		}

		// is args[0] a special value, or generic stage command name?
		cmd := args[0]
		switch {
		case IsAddr(cmd):
			cmd = "tcp"
		case IsFile(cmd):
			cmd = "mrt" // FIXME: stat -> mrt / exec / json / etc.
		default:
			args = args[1:]
		}

		// get s for cmd
		s, err := b.GetStage(idx, cmd)
		if err != nil {
			return err
		}

		// find an explicit end of its args
		var nextargs []string
		for i, arg := range args {
			if arg == "--" {
				nextargs = args[i+1:]
				args = args[:i]
				break
			}
		}

		// parse stage args, move on
		if remargs, err := s.ParseArgs(args); err != nil {
			return err
		} else {
			args = append(remargs, nextargs...)
		}
	}

	return nil
}
