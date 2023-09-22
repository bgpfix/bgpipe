package bgpipe

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
)

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
	f.String("stdio", "auto", "controls stdin and stdout usage (none/auto/in/out)")
	f.BoolP("reverse", "R", false, "reverse the pipe")
	f.BoolP("no-parse-error", "N", false, "silently drop parse error messages")
	f.BoolP("short-asn", "2", false, "use 2-byte ASN numbers")

	// command repository
	b.repo = make(map[string]NewStage)
	for i := range repo {
		b.AddRepo(repo[i])
	}

	return b
}

func (b *Bgpipe) Usage() {
	fmt.Fprintf(os.Stderr, `Usage: bgpipe [OPTIONS] [--] STAGE [STAGE-OPTIONS] [STAGE-ARGUMENTS...] [--] ...

Options:
`)
	b.F.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
Supported stages (run stage -h to get its help)
`)

	// iterate over cmds
	var cmds []string
	for cmd := range b.repo {
		cmds = append(cmds, cmd)
	}
	sort.Strings(cmds)
	for _, cmd := range cmds {
		var descr string

		s := b.NewStage(cmd)
		if s != nil {
			descr = s.Descr
		}

		fmt.Fprintf(os.Stderr, "  %-22s %s\n", cmd, descr)
	}
	fmt.Fprintf(os.Stderr, "\n")
}

func (b *Bgpipe) Run() error {
	// configure
	if err := b.Configure(); err != nil {
		b.Fatal().Err(err).Msg("configuration error")
		return err
	}

	// prepare the pipe
	if err := b.Prepare(); err != nil {
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

func (b *Bgpipe) Configure() error {
	// parse CLI args
	err := b.ParseArgs(os.Args[1:])
	if err != nil {
		return fmt.Errorf("could not parse CLI flags: %w", err)
	}

	// debugging level
	if ll := b.K.String("log"); len(ll) > 0 {
		lvl, err := zerolog.ParseLevel(ll)
		if err != nil {
			return err
		}
		zerolog.SetGlobalLevel(lvl)
	}

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
		b.F.Usage()
		return fmt.Errorf("bgpipe needs at least 1 stage")
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

	// TODO: scan through the pipe, decide if needs automatic stdin/stdout

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
	b.Cancel(nil)
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
