package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
)

// Bgpipe represents a BGP pipeline consisting of several stages, built on top of bgpfix.Pipe
type Bgpipe struct {
	zerolog.Logger
	Version string

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
func NewBgpipe(version string, repo ...map[string]NewStage) *Bgpipe {
	b := &Bgpipe{Version: version}
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

	// print the pipeline and quit?
	if b.K.Bool("explain") {
		fmt.Printf("--> MESSAGES FLOWING RIGHT -->\n")
		b.StageDump(dir.DIR_R, os.Stdout)
		fmt.Printf("\n<-- MESSAGES FLOWING LEFT <--\n")
		b.StageDump(dir.DIR_L, os.Stdout)
		return nil
	}

	// attach our b.Start
	b.Pipe.Options.OnStart(b.onStart)

	// handle signals
	go b.handleSignals()

	// start the pipeline and block
	b.Pipe.Start() // will call b.Start
	b.Pipe.Wait()  // until error or all processing is done

	// wait for all stages to finish
	b.Cancel(ErrPipeFinished)
	for _, s := range b.Stages {
		s.runStop(nil) // may block 1s
	}

	// any errors on the global context?
	err := context.Cause(b.Ctx)
	switch {
	case err == nil || err == ErrPipeFinished: // OK
		return nil // full success
	case errors.Is(err, ErrStageStopped):
		b.Info().Msg(err.Error())
	default:
		b.Error().Err(err).Msg("pipe error")
	}

	return err
}

// handleSignals listens for OS signals
func (b *Bgpipe) handleSignals() {
	// setup signal channel
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)
	defer signal.Stop(ch)

	// wait for signals
	for {
		select {
		case <-b.Ctx.Done():
			return
		case sig := <-ch:
			signal.Reset() // NB: reset to default behavior after first signal
			b.Warn().Stringer("signal", sig).Msg("signal received, stopping...")
			b.Pipe.Stop()
			return
		}
	}
}

// onStart is called after the bgpfix pipe starts
func (b *Bgpipe) onStart(ev *pipe.Event) bool {
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

	if ev.Msg != "" {
		l = l.Str("msg", ev.Msg)
	}

	if ev.Dir != 0 {
		l = l.Stringer("evdir", ev.Dir)
	}

	if ev.Seq != 0 {
		l = l.Uint64("evseq", ev.Seq)
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

// KillEvent brutally kills the session because of given event ev
func (b *Bgpipe) KillEvent(ev *pipe.Event) bool {
	b.LogEvent(ev)
	b.Warn().Stringer("ev", ev).Msg("session killed by event")
	os.Exit(1)
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

// StageCount returns the number of stages added to the pipe
func (b *Bgpipe) StageCount() int {
	return max(0, len(b.Stages)-1)
}

// StageDump prints all stages in dir direction in textual form to w (by default stdout)
func (b *Bgpipe) StageDump(d dir.Dir, w io.Writer) (total int) {
	// use default w?
	if w == nil {
		w = os.Stdout
	}
	colors := w == os.Stdout

	// print function shortcut
	pr := func(style string, format string, a ...any) {
		if colors && style != StyleNone {
			fmt.Fprintf(w, style+format+StyleReset, a...)
		} else {
			fmt.Fprintf(w, format, a...)
		}
	}

	// if only Go had a (simple) reverse iterator...
	indices := make([]int, 0, len(b.Stages))
	for i, s := range b.Stages {
		if s != nil {
			indices = append(indices, i)
		}
	}
	if d == dir.DIR_L {
		slices.Reverse(indices)
	}

	// iterate through stages in good direction
	for i, idx := range indices {
		s := b.Stages[idx]

		// analyze callbacks
		var cb_count int
		var cb_all bool
		var cb_types []msg.Type
		for _, cb := range s.callbacks {
			if cb.Dir != 0 && cb.Dir&d == 0 {
				continue
			}
			cb_count++
			if len(cb.Types) == 0 {
				cb_all = true
			} else {
				cb_types = append(cb_types, cb.Types...)
			}
		}

		// is the last stage and a consumer? treat as a callback
		if s.Options.IsConsumer && i == len(indices)-1 {
			cb_count++
			cb_all = true
		}

		// analyze inputs
		var in_count int
		for _, in := range s.inputs {
			if in.Dir&d == 0 {
				continue
			}
			in_count++
		}

		// analyze event handlers
		var eh_count int
		var eh_all bool
		var eh_types []string
		for _, eh := range s.handlers {
			if eh.Dir != 0 && eh.Dir&d == 0 {
				continue
			}
			eh_count++
			if len(eh.Types) == 0 {
				eh_all = true
			} else {
				eh_types = append(eh_types, eh.Types...)
			}
		}

		// should skip?
		switch {
		case cb_count > 0:
			total++ // has callbacks in this direction
		case in_count > 0:
			total++ // has inputs in this direction
		case len(s.callbacks)+len(s.inputs) == 0 && eh_count > 0:
			total++ // no inputs or callbacks at all, but reacts to events
		default:
			continue // skip
		}

		pr(StyleNone, "  [%d] ", s.Index)
		pr(StyleBold, "%s", s.Name)
		// pr(StyleGreen, " -%s", s.Dir)
		if len(s.Flags) > 0 {
			pr(StyleGreen, " %s", strings.Join(s.Flags, " "))
		}
		for _, arg := range s.Args {
			pr(StyleNone, " ")
			pr(StyleRed+StyleUnderline, "%s", arg)
		}
		pr(StyleNone, "\n")

		if cb_count > 0 {
			pr(StyleNone, "      reads messages from pipeline")
			pr(StyleMagenta, " callbacks=%d", cb_count)
			if !cb_all {
				slices.Sort(cb_types)
				pr(StyleMagenta, " types=%v", slices.Compact(cb_types))
			} else {
				pr(StyleMagenta, " types=[ALL]")
			}
			pr(StyleNone, "\n")
		}

		if in_count > 0 {
			pr(StyleNone, "      writes messages to pipeline")
			pr(StyleMagenta, " inputs=%d\n", in_count)
		}

		if eh_count > 0 {
			pr(StyleNone, "      handles events")
			pr(StyleMagenta, " handlers=%d", eh_count)
			if !eh_all {
				slices.Sort(eh_types)
				pr(StyleMagenta, " types=%v", slices.Compact(eh_types))
			} else {
				pr(StyleMagenta, " types=[ALL]")
			}
			pr(StyleNone, "\n")
		}
	}

	if total == 0 {
		pr(StyleNone, "  (none)\n")
	}

	return total
}
