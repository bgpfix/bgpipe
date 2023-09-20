package bgpipe

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Bgpipe struct {
	zerolog.Logger
	ctx    context.Context
	cancel context.CancelCauseFunc

	K      *koanf.Koanf // global config
	Pipe   *pipe.Pipe   // bgpfix pipe
	Stages []*StageBase // pipe stages

	wg_lwrite sync.WaitGroup // stages that write to pipe L
	wg_lread  sync.WaitGroup // stages that read from pipe L
	wg_rwrite sync.WaitGroup // stages that write to pipe R
	wg_rread  sync.WaitGroup // stages that read from pipe R
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

	// run the pipe and block till end
	b.Pipe.Start() // will call b.OnStart
	b.Pipe.Wait()

	// any errors on the global context?
	if err := context.Cause(b.ctx); err != nil {
		b.Fatal().Err(err).Msg("fatal error")
		return err
	}

	// successfully finished
	return nil
}

func (b *Bgpipe) Prepare() error {
	// shortcuts
	var (
		k  = b.K
		p  = b.Pipe
		po = &p.Options
	)

	// prepare stages
	for _, s := range b.Stages {
		if s == nil {
			continue
		}

		if err := s.Prepare(); err != nil {
			return s.Errorf("%w", err)
		} else {
			b.Debug().Interface("koanf", s.K.All()).Msgf("initialized %s", s.Name)
		}
	}

	// force 2-byte ASNs?
	if k.Bool("short-asn") {
		p.Caps.Set(caps.CAP_AS4, nil) // ban CAP_AS4
	} else {
		p.Caps.Use(caps.CAP_AS4) // use CAP_AS4 by default
	}

	// attach to events
	po.OnStart(b.OnStart) // pipe.EVENT_START
	// TODO: EVENT_ESTABLISHED, EVENT_OPEN_*
	if !k.Bool("perr") {
		po.OnParseError(b.OnParseError) // pipe.EVENT_PARSE
	}

	// FIXME
	// TODO: scan through the pipe, decide if needs automatic stdin/stdout
	po.OnMsgLast(b.print, msg.DST_LR)

	return nil
}

// OnStart is called after the bgpfix pipe starts
func (b *Bgpipe) OnStart(ev *pipe.Event) bool {
	// go through all stages
	for _, s := range b.Stages {
		if s == nil {
			continue
		}

		// kick waitgroups
		if s.IsLReader() {
			b.wg_lread.Add(1)
		}
		if s.IsLWriter() {
			b.wg_lwrite.Add(1)
		}
		if s.IsRReader() {
			b.wg_rread.Add(1)
		}
		if s.IsRWriter() {
			b.wg_rwrite.Add(1)
		}

		// TODO: support waiting for OPEN (L/R/LR) or ESTABLISH or FIRST_MSG?
		go s.Start()
	}

	// wait for L/R writers
	go func() {
		b.wg_lwrite.Wait()
		b.Debug().Msg("closing L input (no writers)")
		b.Pipe.L.CloseInput()
	}()
	go func() {
		b.wg_rwrite.Wait()
		b.Debug().Msg("closing R input (no writers)")
		b.Pipe.R.CloseInput()
	}()

	// wait for L/R readers
	go func() {
		b.wg_lread.Wait()
		b.Debug().Msg("closing L output (no readers)")
		b.Pipe.L.CloseOutput()
	}()
	go func() {
		b.wg_rread.Wait()
		b.Debug().Msg("closing R output (no readers)")
		b.Pipe.R.CloseOutput()
	}()

	return true
}

func (b *Bgpipe) OnStop(ev *pipe.Event) bool {
	b.Info().Msg("pipe stopped")
	b.cancel(nil)
	return true
}

// OnParseError is called when the pipe sees a message it cant parse
func (b *Bgpipe) OnParseError(ev *pipe.Event) bool {
	b.Error().
		Str("msg", ev.Msg.String()).
		Err(ev.Error).
		Msg("message parse error")
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
