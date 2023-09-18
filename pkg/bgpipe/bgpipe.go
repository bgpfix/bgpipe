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

	K      *koanf.Koanf // global config
	Pipe   *pipe.Pipe   // bgpfix pipe
	Stage2 []*Stage     // pipe stages
	Last   int          // idx of the last stage

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

	// global config
	b.K = koanf.New(".")

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
	for _, s := range b.Stage2 {
		if s == nil {
			continue
		}

		if err := s.Prepare(); err != nil {
			return s.Errorf("%w", err)
		} else {
			b.Debug().
				Interface("koanf", s.K.All()).
				Msgf("initialized %s", s.Name)
		}
	}

	// attach to pipe
	po := &b.Pipe.Options

	// pipe.EVENT_START
	po.OnStart(b.OnStart)

	// TODO: scan through the pipe, decide

	// FIXME
	po.OnMsgLast(b.print, msg.DST_LR)

	return nil
}

// OnStart is called after the bgpfix pipe starts
func (b *Bgpipe) OnStart(ev *pipe.Event) bool {
	// start all stages inside eg
	var lread, rread bool // has L/R reader?
	for _, s := range b.Stage2 {
		if s == nil {
			continue
		}

		// is reader?
		if s.IsReader {
			lread = lread || s.K.Bool("left")
			rread = rread || s.K.Bool("right")
		}

		// TODO: wait for OPEN?
		b.eg.Go(s.Start)
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
