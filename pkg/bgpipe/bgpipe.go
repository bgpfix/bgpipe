package bgpipe

import (
	"context"
	"fmt"
	"os"

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
	// FIXME: only if there is no sink at R
	if len(b.Stages) == 1 {
		b.Pipe.R.CloseOutput()
	}

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
	po.OnMsgLast(b.print, msg.DST_X)

	return nil
}
