package bgpipe

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

type Bgpipe struct {
	zerolog.Logger
	ctx    context.Context
	cancel context.CancelCauseFunc

	Koanf *koanf.Koanf
	Pipe  *pipe.Pipe
	Steps []Step // pipe steps
}

func NewBgpipe(ctx context.Context) *Bgpipe {
	b := new(Bgpipe)
	b.ctx, b.cancel = context.WithCancelCause(ctx)
	b.Logger = log.Logger
	return b
}

func (b *Bgpipe) Run() error {
	// configure
	if err := b.Configure(); err != nil {
		b.Fatal().Err(err).Msg("configuration error")
		return err
	}

	// prepare the pipe
	if err := b.Attach(); err != nil {
		b.Fatal().Err(err).Msg("could not setup the pipe")
		return err
	}

	// run the pipe
	group, gctx := errgroup.WithContext(b.ctx)
	for _, step := range b.Steps {
		group.Go(step.Run)
	}

	// block until done
	<-gctx.Done()
	b.cancel(nil) // TODO?

	// any errors?
	if err := group.Wait(); err != nil {
		b.Fatal().Err(err).Msg("could not run the pipe")
		return err
	}

	return nil
}

func (b *Bgpipe) Attach() error {
	// create a new BGP pipe
	p := pipe.NewPipe(b.ctx)
	po := &p.Options
	po.Logger = b.Logger
	po.Tstamp = true

	// attach steps
	for pos, step := range b.Steps {
		err := step.Attach(p)
		if err != nil {
			return fmt.Errorf("%s[%d]: %w", step.Name(), pos, err)
		}
	}

	return nil
}

func (b *Bgpipe) Configure() error {
	b.Koanf = koanf.New(".")

	// hard-coded defaults
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	b.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.DateTime,
	})

	// parse CLI args
	err := b.ParseArgs(os.Args[1:])
	if err != nil {
		return fmt.Errorf("could not parse CLI flags: %w", err)
	}

	// FIXME: analyze the config and decide if OK and a speaker needed

	return nil
}

func (b *Bgpipe) ParseArgs(args []string) error {
	// setup CLI flag parser
	f := pflag.NewFlagSet("bgpipe", pflag.ExitOnError)
	f.SetInterspersed(false)
	f.String("out", "both", "stdout output control (both/tx/rx/none)")
	f.String("in", "tx", "stdin input control (tx/rx/none)")

	// parse CLI flags
	if err := f.Parse(args); err != nil {
		return err
	}

	// export flags into koanf
	b.Koanf.Load(posflag.Provider(f, ".", b.Koanf), nil)

	// parse steps and their args
	args = f.Args()
	for pos := 0; len(args) > 0; pos++ {
		// skip empty steps
		if args[0] == "--" {
			args = args[1:]
			continue
		}

		// is args[0] a special value, or generic step command name?
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

		// already defined?
		var s Step
		if pos < len(b.Steps) {
			s = b.Steps[pos]
		}

		// create new instance and store?
		if s == nil {
			// cmd valid?
			newfunc, ok := NewStepFuncs[cmd]
			if !ok {
				return fmt.Errorf("step[%d]: invalid command '%s'", pos, cmd)
			}
			s = newfunc(b, cmd, pos)

			// store
			if pos < len(b.Steps) {
				b.Steps[pos] = s
			} else {
				b.Steps = append(b.Steps, s)
			}
		}

		// parse step args
		err := s.ParseArgs(args[:end])
		if err != nil {
			return fmt.Errorf("%s[%d]: %w", cmd, pos, err)
		}

		// next args
		args = args[end:]
	}

	return nil
}
