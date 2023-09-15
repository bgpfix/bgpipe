package bgpipe

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

type Bgpipe struct {
	zerolog.Logger
	ctx    context.Context
	cancel context.CancelCauseFunc

	Pipe *pipe.Pipe   // bgpfix pipe
	K    *koanf.Koanf // shortcut to b.Koanf[0]

	Stage []Stage        // stage implementations, [0] is not used
	Koanf []*koanf.Koanf // stage configs, [0] is root
	Last  int            // idx of the last stage

	eg  *errgroup.Group // errgroup running the stages
	egx context.Context // eg context
}

func NewBgpipe() *Bgpipe {
	b := new(Bgpipe)
	b.ctx, b.cancel = context.WithCancelCause(context.Background())

	// default logger
	b.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.DateTime,
	})

	// pipe
	b.Pipe = pipe.NewPipe(b.ctx)
	b.Pipe.Options.Logger = &b.Logger

	// stages
	b.Stage = make([]Stage, 1)        // [0] is not used
	b.Koanf = make([]*koanf.Koanf, 1) // [0] is root

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
	// FIXME: allow for stages to exit with nil errors just fine
	<-b.egx.Done()
	b.cancel(fmt.Errorf("shutdown")) // stop the rest, if needed

	// report
	if err := b.eg.Wait(); err != nil {
		b.Fatal().Err(err).Msg("fatal error")
		return err
	}

	return nil
}

func (b *Bgpipe) Prepare() error {
	// prepare stages
	for idx, s := range b.Stage {
		if s == nil {
			continue
		}

		// direction
		k := b.Koanf[idx]
		left, right := k.Bool("left"), k.Bool("right")
		switch idx {
		case 1: // first
			if left {
				return fmt.Errorf("%s: invalid L direction for first stage", s.Name())
			}
			k.Set("right", true)
		case b.Last:
			if right {
				return fmt.Errorf("%s: invalid R direction for last stage", s.Name())
			}
			k.Set("left", true)
		default:
			if left || right {
				// ok, take it
			} else {
				k.Set("right", true) // by default send to R
			}
		}

		// needs raw access?
		if s.IsRaw() && idx != 1 && idx != b.Last {
			return fmt.Errorf("%s: must be either the first or the last stage", s.Name())
		}

		// init
		err := s.Init()
		if err != nil {
			return fmt.Errorf("%s: %w", s.Name(), err)
		}
		b.Debug().
			Interface("koanf", b.Koanf[idx].All()).
			Msgf("initialized %s", s.Name())
	}

	// attach to pipe
	po := &b.Pipe.Options

	// pipe.EVENT_START
	po.OnStart(b.OnStart)

	// FIXME
	po.OnMsgLast(b.print, msg.DST_LR)

	return nil
}

func (b *Bgpipe) OnStart(ev *pipe.Event) bool {
	// start all stages inside eg
	var lread, rread bool // has L/R reader?
	for _, s := range b.Stage {
		if s == nil {
			continue
		}
		b.eg.Go(s.Start)

		l, r := s.IsReader()
		if l {
			lread = true
		}
		if r {
			rread = true
		}
	}

	// nothing reading the pipe end?
	if !lread {
		b.Pipe.L.CloseOutput()
	}
	if !rread {
		b.Pipe.R.CloseOutput()
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
