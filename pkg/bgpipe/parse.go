package bgpipe

import (
	"github.com/knadh/koanf/providers/posflag"
)

func (b *Bgpipe) ParseArgs(args []string) error {
	// parse and export flags into koanf
	if err := b.F.Parse(args); err != nil {
		return err
	} else {
		b.K.Load(posflag.Provider(b.F, ".", b.K), nil)
	}

	// parse stages and their args
	args = b.F.Args()
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
			cmd = "mrt" // TODO: stat -> mrt / exec / json / etc.
		default:
			args = args[1:]
		}

		// get s for cmd
		s, err := b.GetStage(idx, cmd)
		if err != nil {
			return err
		}

		// find an explicit end of its args
		var found bool
		var nextargs []string
		for i, arg := range args {
			if arg == "--" {
				found = true
				nextargs = args[i+1:]
				args = args[:i]
				break
			}
		}

		// parse stage args, move on
		if remargs, err := s.ParseArgs(args, found); err != nil {
			return err
		} else {
			args = append(remargs, nextargs...)
		}
	}

	return nil
}
