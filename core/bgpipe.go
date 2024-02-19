package bgpipe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
)

// Bgpipe represents a BGP pipeline consisting of several stages, built on top of bgpfix.Pipe
type Bgpipe struct {
	zerolog.Logger

	Ctx    context.Context
	Cancel context.CancelCauseFunc

	F      *pflag.FlagSet // global flags
	K      *koanf.Koanf   // global config
	Pipe   *pipe.Pipe     // bgpfix pipe
	Stages []*StageBase   // pipe stages

	repo map[string]NewStage // maps cmd to new stage func

	wg_lwrite sync.WaitGroup // stages that write to pipe L
	wg_lread  sync.WaitGroup // stages that read from pipe L
	wg_rwrite sync.WaitGroup // stages that write to pipe R
	wg_rread  sync.WaitGroup // stages that read from pipe R

	logbuf []byte // buffer for LogEvent
}

// NewBgpipe creates a new bgpipe instance using given
// repositories of stage commands
func NewBgpipe(repo ...map[string]NewStage) *Bgpipe {
	b := new(Bgpipe)
	b.Ctx, b.Cancel = context.WithCancelCause(context.Background())

	// default logger
	b.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.DateTime,
	})

	// pipe
	b.Pipe = pipe.NewPipe(b.Ctx)
	po := &b.Pipe.Options
	po.Logger = &b.Logger

	// global config
	b.K = koanf.New(".")

	// global CLI flags
	b.F = pflag.NewFlagSet("bgpipe", pflag.ExitOnError)
	b.addFlags()

	// command repository
	b.repo = make(map[string]NewStage)
	for i := range repo {
		b.AddRepo(repo[i])
	}

	return b
}

// Run configures and runs the bgpipe
func (b *Bgpipe) Run() error {
	// configure bgpipe and its stages
	if err := b.Configure(); err != nil {
		b.Error().Err(err).Msg("configuration error")
		return err
	}

	// attach stages to pipe
	if err := b.AttachStages(); err != nil {
		b.Error().Err(err).Msg("could not attach stages to the pipe")
		return err
	}

	// attach our b.Start
	b.Pipe.Options.OnStart(b.Start)

	// start the pipeline and block
	b.Pipe.Start() // will call b.Start
	b.Pipe.Wait()  // until error or all processing is done

	// TODO: wait until all pipe output is read

	// any errors on the global context?
	err := context.Cause(b.Ctx)
	switch {
	case err == nil:
		break // full success
	case errors.Is(err, ErrStageStopped):
		b.Info().Msg(err.Error())
	default:
		b.Error().Err(err).Msg("pipe error")
	}

	return err
}

// Start is called after the bgpfix pipe starts
func (b *Bgpipe) Start(ev *pipe.Event) bool {
	// wait for writers
	go func() {
		b.wg_lwrite.Wait()
		b.Debug().Msg("closing L inputs")
		b.Pipe.L.Close()
	}()
	go func() {
		b.wg_rwrite.Wait()
		b.Debug().Msg("closing R inputs")
		b.Pipe.R.Close()
	}()

	// wait for readers
	go func() {
		b.wg_lread.Wait()
		b.Debug().Msg("closing L output")
		b.Pipe.L.CloseOutput()
	}()
	go func() {
		b.wg_rread.Wait()
		b.Debug().Msg("closing R output")
		b.Pipe.R.CloseOutput()
	}()

	return false
}

// LogEvent logs given event
func (b *Bgpipe) LogEvent(ev *pipe.Event) bool {
	// will b.Info() if ev.Error is nil
	l := b.Err(ev.Error)

	if ev.Msg != nil {
		b.logbuf = ev.Msg.ToJSON(b.logbuf[:0])
		l = l.Bytes("msg", b.logbuf)
	}

	if ev.Dir != 0 {
		l = l.Stringer("dir", ev.Dir)
	}

	if ev.Seq != 0 {
		l = l.Uint64("seq", ev.Seq)
	}

	if vals, ok := ev.Value.([]any); ok && len(vals) > 0 {
		is := len(vals) - 1
		if s, _ := vals[is].(*StageBase); s != nil {
			l = l.Stringer("stage", s)
			vals = vals[:is]
		}
		l = l.Interface("vals", vals)
	}

	l.Msgf("event %s", ev.Type)
	return true
}

// KillEvent kills session because of given event ev
func (b *Bgpipe) KillEvent(ev *pipe.Event) bool {
	b.Cancel(fmt.Errorf("%w: %s", ErrKill, ev))
	// TODO: why not pipe.Stop()?
	return false
}

// AddRepo adds mapping between stage commands and their NewStageFunc
func (b *Bgpipe) AddRepo(cmds map[string]NewStage) {
	for cmd, newfunc := range cmds {
		b.repo[cmd] = newfunc
	}
}

// AddStage adds and returns a new stage at idx for cmd,
// or returns an existing instance if it's for the same cmd.
func (b *Bgpipe) AddStage(idx int, cmd string) (*StageBase, error) {
	// append?
	if idx <= 0 {
		idx = max(1, len(b.Stages))
	}

	// already there? check cmd
	if idx < len(b.Stages) {
		if s := b.Stages[idx]; s != nil {
			if s.Cmd == cmd {
				return s, nil
			} else {
				return nil, fmt.Errorf("[%d] %s: %w: %s", idx, cmd, ErrStageDiff, s.Cmd)
			}
		}
	}

	// create
	s := b.NewStage(cmd)
	if s == nil {
		return nil, fmt.Errorf("[%d] %s: %w", idx, cmd, ErrStageCmd)
	}

	// store
	for idx >= len(b.Stages) {
		b.Stages = append(b.Stages, nil)
	}
	b.Stages[idx] = s
	s.Index = idx

	return s, nil
}

func (b *Bgpipe) StageCount() int {
	return max(0, len(b.Stages)-1)
}
