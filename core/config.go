package core

import (
	"fmt"
	"os"
	"runtime/debug"
	"slices"
	"strings"

	"net/http"
	_ "net/http/pprof"

	"github.com/bgpfix/bgpfix/filter"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/rs/zerolog"
)

// Configure configures bgpipe
func (b *Bgpipe) Configure() error {
	// parse CLI args
	err := b.parseArgs(os.Args[1:])
	if err != nil {
		return fmt.Errorf("could not parse CLI flags: %w", err)
	}
	k := b.K

	// debugging level
	if ll := k.String("log"); len(ll) > 0 {
		lvl, err := zerolog.ParseLevel(ll)
		if err != nil {
			return err
		}
		zerolog.SetGlobalLevel(lvl)
	}

	// pprof?
	if v := k.String("pprof"); len(v) > 0 {
		go func() {
			b.Fatal().Err(http.ListenAndServe(v, nil)).Msg("pprof failed")
		}()
	}

	// capabilities?
	switch v := k.String("caps"); {
	case len(v) == 0: // none
		break
	case v[0] == '@': // read from file
		jsv, err := os.ReadFile(v[1:])
		if err != nil {
			return fmt.Errorf("could not read --caps file: %w", err)
		}
		if err := b.Pipe.Caps.FromJSON(jsv); err != nil {
			return fmt.Errorf("could not parse --caps file: %w", err)
		}
	default: // parse JSON
		if err := b.Pipe.Caps.FromJSON([]byte(v)); err != nil {
			return fmt.Errorf("could not parse --caps: %w", err)
		}
	}

	return nil
}

func (b *Bgpipe) addFlags() {
	f := b.F
	f.SortFlags = false
	f.Usage = b.usage
	f.SetInterspersed(false)
	f.BoolP("version", "v", false, "print detailed version info and quit")
	f.BoolP("explain", "n", false, "print the pipeline as configured and quit")
	f.StringP("log", "l", "info", "log level (debug/info/warn/error/disabled)")
	f.String("pprof", "", "bind pprof to given listen address")
	f.StringSliceP("events", "e", []string{"PARSE", "ESTABLISHED", "EOR"}, "log given events (\"all\" means all events)")
	f.StringSliceP("kill", "k", nil, "kill session on any of these events")
	f.BoolP("stdin", "i", false, "read JSON from stdin")
	f.BoolP("stdout", "o", false, "write JSON to stdout")
	f.BoolP("stdin-wait", "I", false, "like --stdin but wait for EVENT_ESTABLISHED")
	f.BoolP("stdout-wait", "O", false, "like --stdout but wait for EVENT_EOR")
	f.BoolP("short-asn", "2", false, "use 2-byte ASN numbers")
	f.String("caps", "", "use given BGP capabilities (JSON format)")
}

func (b *Bgpipe) usage() {
	fmt.Fprintf(os.Stderr, `Usage: bgpipe [OPTIONS] [--] STAGE1 [OPTIONS] [ARGUMENTS] [--] STAGE2...

Options:
`)
	b.F.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
Supported stages (run <stage> -h to get its help)
`)

	// iterate over cmds
	var cmds []string
	for cmd := range b.repo {
		cmds = append(cmds, cmd)
	}
	slices.Sort(cmds)
	for _, cmd := range cmds {
		var descr string

		s := b.NewStage(cmd)
		if s != nil {
			descr = s.Options.Descr
		}

		fmt.Fprintf(os.Stderr, "  %-22s %s\n", cmd, descr)
	}
	fmt.Fprintf(os.Stderr, "\n")
}

// Usage prints usage screen to stderr
func (s *StageBase) usage() {
	var (
		o = &s.Options
		f = o.Flags
		e = os.Stderr
	)

	if len(o.Usage) > 0 {
		fmt.Fprintf(e, "Stage usage: %s", o.Usage)
	} else {
		fmt.Fprintf(e, "Stage usage: %s [OPTIONS] %s",
			s.Cmd, strings.ToUpper(strings.Join(o.Args, " ")))
	}
	fmt.Fprintf(e, "\n\nDescription: %s\n", o.Descr)

	for i, l := range strings.Split(f.FlagUsages(), "\n") {
		if strings.HasPrefix(l, "  -L, --left") {
			fmt.Fprint(e, "\nCommon Options:\n")
		} else if i == 0 {
			fmt.Fprint(e, "\nOptions:\n")
		}
		fmt.Fprintf(e, "%s\n", l)
	}

	// iterate over events?
	if len(o.Events) > 0 {
		fmt.Fprint(e, "Events:\n")
		var events []string
		for e := range o.Events {
			events = append(events, e)
		}
		slices.Sort(events)
		for _, ev := range events {
			fmt.Fprintf(e, "  %-24s %s\n", s.Name+"/"+ev, o.Events[ev])
		}
		fmt.Fprint(e, "\n")
	}
}

// parseArgs adds and configures stages from CLI args
func (b *Bgpipe) parseArgs(args []string) error {
	// parse and export flags into koanf
	if err := b.F.Parse(args); err != nil {
		return err
	} else {
		b.K.Load(posflag.Provider(b.F, ".", b.K), nil)
	}

	// print version and quit?
	if b.K.Bool("version") {
		if bi, ok := debug.ReadBuildInfo(); ok && bi != nil {
			fmt.Fprintf(os.Stderr, "bgpipe build info:\n%s", bi)
		}
		os.Exit(1)
	}

	// parse stages and their args
	args = b.F.Args()
	for idx := 1; len(args) > 0; idx++ {
		// skip empty stages
		if args[0] == "--" {
			args = args[1:]
			continue
		}

		// has a name prefix?
		name := ""
		if args[0][0] == '@' {
			name = args[0]
			args = args[1:]
		}

		// is args[0] a special value, or generic stage command name?
		cmd := args[0]
		switch {
		case IsAddr(cmd):
			cmd = "connect"
		case IsBind(cmd):
			cmd = "listen"
		case IsFile(cmd):
			cmd = "read" // TODO: stat -> mrt / json / exec / etc.
			args = slices.Insert(args, 0, "--mrt")
		default:
			args = args[1:]
		}

		// get s for cmd
		s, err := b.AddStage(idx, cmd)
		if err != nil {
			return err
		}

		// override the stage name?
		if name != "" {
			s.Name = name
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
		if remargs, err := s.parseArgs(args); err != nil {
			return err
		} else {
			args = append(remargs, nextargs...)
		}
	}

	return nil
}

// parseArgs parses CLI flags and arguments and exports to s.K.
// May return unused args.
func (s *StageBase) parseArgs(args []string) (unused []string, err error) {
	o := &s.Options
	f := o.Flags

	// override f.Usage?
	if f.Usage == nil {
		f.Usage = s.usage
	}

	// parse stage flags
	if err := f.Parse(args); err != nil {
		return args, s.Errorf("%w", err)
	}

	// export flags to koanf, collect remaining args
	s.K.Load(posflag.Provider(f, ".", s.K), nil)
	rem := f.Args()

	// parse the stage input filter?
	if v := s.K.String("if"); len(v) > 0 {
		s.flt_in, err = filter.NewFilter(v)
		if err != nil {
			return rem, s.Errorf("could not parse --if: %w", err)
		}
	}

	// parse the stage output filter?
	if v := s.K.String("of"); len(v) > 0 {
		s.flt_out, err = filter.NewFilter(v)
		if err != nil {
			return rem, s.Errorf("could not parse --of: %w", err)
		}
	}

	// compare original args vs remaining -> consumed flags
	consumed := max(0, len(args)-len(rem))
	s.Flags = args[:consumed]

	// rewrite required arguments?
	for _, name := range o.Args {
		if len(rem) == 0 {
			return rem, s.Errorf("needs an argument: %s", name)
		}
		s.K.Set(name, rem[0])
		s.Args = append(s.Args, rem[0])
		rem = rem[1:]
	}

	// consume the rest of arguments?
	if v, _ := f.GetBool("args"); v {
		s.K.Set("args", rem)
		s.Args = append(s.Args, rem...)
		return nil, nil
	}

	return rem, nil
}
