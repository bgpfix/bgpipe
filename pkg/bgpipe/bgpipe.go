package bgpipe

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bgpfix/bgpfix/msg"
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

	Koanf  *koanf.Koanf // global config
	Pipe   *pipe.Pipe   // bgpfix pipe
	Stages []Stage      // pipe stages
	Last   int          // idx of the last stage

	eg  *errgroup.Group // errgroup running the stages
	egx context.Context // eg context
}

func NewBgpipe() *Bgpipe {
	b := new(Bgpipe)
	b.ctx, b.cancel = context.WithCancelCause(context.Background())
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
	if err := b.Prepare(); err != nil {
		b.Fatal().Err(err).Msg("could not prepare")
		return err
	}

	// run the pipe
	b.eg, b.egx = errgroup.WithContext(b.ctx)
	b.Pipe.Start() // will call b.OnStart

	// block until anything under eg stops
	<-b.egx.Done()
	b.cancel(fmt.Errorf("shutdown")) // stop the rest, if needed

	// report
	if err := b.eg.Wait(); err != nil {
		b.Fatal().Err(err).Msg("fatal error")
		return err
	}

	return nil
}

func (b *Bgpipe) OnStart(ev *pipe.Event) bool {
	// start all stages inside eg
	for _, stage := range b.Stages {
		b.eg.Go(stage.Start)
	}

	// needed to cancel egx when all stages finish without an error
	go b.eg.Wait()

	return true
}

// FIXME
var printbuf []byte

func (b *Bgpipe) print(m *msg.Msg) pipe.Action {
	printbuf = m.ToJSON(printbuf[:0])
	printbuf = append(printbuf, '\n')
	os.Stdout.Write(printbuf)
	return pipe.ACTION_CONTINUE
}

func (b *Bgpipe) Prepare() error {
	// create a new BGP pipe
	b.Pipe = pipe.NewPipe(b.ctx)
	po := &b.Pipe.Options
	po.Logger = b.Logger
	po.Tstamp = true

	// prepare stages
	for _, stage := range b.Stages {
		err := stage.Prepare(b.Pipe)
		if err != nil {
			return fmt.Errorf("%s: %w", stage.Name(), err)
		}
	}

	// attach to pipe.EVENT_START
	po.OnStart(b.OnStart)

	// FIXME
	po.OnLast(b.print, msg.DST_X)

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

	// at least one stage defined?
	if len(b.Stages) == 0 {
		return fmt.Errorf("need at least 1 pipe stage")
	} else {
		b.Last = len(b.Stages) - 1
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
			cmd = "connect"
		case IsFile(args[0]):
			cmd = "file" // FIXME: stat -> mrt / exec / json / etc.
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
		var s Stage
		if idx < len(b.Stages) {
			s = b.Stages[idx]
		}

		// create new instance and store?
		if s == nil {
			// cmd valid?
			newfunc, ok := NewStageFuncs[cmd]
			if !ok {
				return fmt.Errorf("[%d]: invalid stage '%s'", idx, cmd)
			}
			s = newfunc(b, cmd, idx)

			// store
			if idx < len(b.Stages) {
				b.Stages[idx] = s
			} else {
				b.Stages = append(b.Stages, s)
			}
		}

		// parse stage args
		err := s.ParseArgs(args[:end])
		if err != nil {
			return fmt.Errorf("%s: %w", s.Name(), err)
		}

		// next args
		args = args[end:]
	}

	return nil
}
