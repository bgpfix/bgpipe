package bgpipe

import (
	"context"
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
	b.Pipe.Options.Logger = &b.Logger

	// global config
	b.K = koanf.New(".")

	// global CLI flags
	b.F = pflag.NewFlagSet("bgpipe", pflag.ExitOnError)
	f := b.F
	f.SortFlags = false
	f.Usage = b.Usage
	f.SetInterspersed(false)
	f.StringP("log", "L", "warn", "log level (debug/info/warn/error/disabled)")
	f.BoolP("quiet", "N", false, "do not use stdin/stdout unless explicitly requested")
	f.BoolP("reverse", "R", false, "reverse the pipe")
	f.BoolP("no-parse-error", "E", false, "silently drop parse error messages")
	f.BoolP("short-asn", "2", false, "use 2-byte ASN numbers")

	// command repository
	b.repo = make(map[string]NewStage)
	for i := range repo {
		b.AddRepo(repo[i])
	}

	return b
}

// Run configures and runs the bgpipe
func (b *Bgpipe) Run() error {
	// configure
	if err := b.pipeConfig(); err != nil {
		b.Fatal().Err(err).Msg("configuration error")
		return err
	}

	// prepare the pipe
	if err := b.pipePrepare(); err != nil {
		b.Fatal().Err(err).Msg("could not prepare the pipeline")
		return err
	}

	// run the pipe and block till end
	b.Pipe.Start() // will call b.OnStart
	b.Pipe.Wait()

	// any errors on the global context?
	if err := context.Cause(b.Ctx); err != nil {
		b.Fatal().Err(err).Msg("fatal error")
		return err
	}

	// successfully finished
	return nil
}

// AddRepo adds mapping between stage commands and their NewStageFunc
func (b *Bgpipe) AddRepo(cmds map[string]NewStage) {
	for cmd, newfunc := range cmds {
		b.repo[cmd] = newfunc
	}
}

// AddStage adds and returns a new stage at idx for cmd,
// or returns an existing instance if it's for the same cmd.
// If idx is -1, it always appends a new stage.
func (b *Bgpipe) AddStage(idx int, cmd string) (*StageBase, error) {
	if idx == -1 {
		// append new
		idx = len(b.Stages)
	} else if idx < len(b.Stages) {
		// already there? check cmd
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

	s.Idx = idx
	s.SetName(fmt.Sprintf("[%d] %s", idx, cmd))

	return s, nil
}
