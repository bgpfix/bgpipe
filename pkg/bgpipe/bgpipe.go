package bgpipe

import (
	"context"
	"fmt"
	"os"
	"slices"
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

	// at least one stage defined?
	if len(b.Stages) == 0 {
		return fmt.Errorf("need at least 1 stage")
	}

	// reverse?
	if k.Bool("reverse") {
		slices.Reverse(b.Stages)
		for idx, s := range b.Stages {
			if s != nil {
				s.Idx = idx
				s.SetName(fmt.Sprintf("[%d] %s", idx, s.Cmd))
			}
		}
		left, right := k.Bool("left"), k.Bool("right")
		k.Set("left", right)
		k.Set("right", left)
	}

	// prepare stages
	for _, s := range b.Stages {
		if s == nil {
			continue
		}

		if err := s.Prepare(); err != nil {
			return s.Errorf("%w", err)
		}
	}

	// force 2-byte ASNs?
	if k.Bool("short-asn") {
		p.Caps.Set(caps.CAP_AS4, nil) // ban CAP_AS4
	} else {
		p.Caps.Use(caps.CAP_AS4) // use CAP_AS4 by default
	}

	// attach to events
	po.OnStart(b.OnStart)
	po.OnEstablished(b.OnEstablished)
	if !k.Bool("perr") {
		po.OnParseError(b.OnParseError) // pipe.EVENT_PARSE
	}

	// FIXME
	// TODO: scan through the pipe, decide if needs automatic stdin/stdout
	po.OnMsgLast(b.printL, msg.DST_L)
	po.OnMsgLast(b.printR, msg.DST_R)

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
		if !s.K.Bool("wait") {
			go s.Start()
		}
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

	return false
}
func (b *Bgpipe) OnEstablished(ev *pipe.Event) bool {
	for _, s := range b.Stages {
		if s == nil {
			continue
		}

		if s.K.Bool("wait") {
			go s.Start()
		}
	}

	return false
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
var bufL, bufR []byte

func (b *Bgpipe) printL(m *msg.Msg) pipe.Action {
	bufL = m.ToJSON(bufL[:0])
	bufL = append(bufL, '\n')
	os.Stdout.Write(bufL)
	return pipe.ACTION_CONTINUE
}

func (b *Bgpipe) printR(m *msg.Msg) pipe.Action {
	bufR = m.ToJSON(bufR[:0])
	bufR = append(bufR, '\n')
	os.Stdout.Write(bufR)
	return pipe.ACTION_CONTINUE
}
