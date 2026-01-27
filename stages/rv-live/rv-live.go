// Package rvlive reads BGP updates from RouteViews.org via Kafka (OpenBMP format)
package rvlive

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/bmp"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

// RvLive reads BGP updates from RouteViews.org via Kafka (OpenBMP format)
type RvLive struct {
	*core.StageBase
	in *pipe.Input

	broker     string
	topics     string
	topicsRe   *regexp.Regexp
	group      string
	stateFile  string
	refresh    time.Duration
	timeout    time.Duration
	stale      time.Duration
	retry      bool
	retryMax   int
	keepAspath bool

	// state management
	state      *rvState
	stateMu    sync.Mutex
	stateDirty bool

	// reusable parsers
	obmpMsg *bmp.OpenBmp
	bmpMsg  *bmp.Bmp
}

func NewRvLive(parent *core.StageBase) core.Stage {
	var (
		s = &RvLive{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	o.Descr = "read BGP updates from RouteViews.org via Kafka"
	o.IsProducer = true
	o.FilterOut = true

	f.String("broker", "stream.routeviews.org:9092", "Kafka broker address")
	f.String("topics", `^routeviews\..+\.bmp_raw$`, "topic regex pattern")
	f.String("router", "", "consume only from specific router (instead of topics regex)")
	f.String("group", "", "consumer group ID (auto-generated if empty)")
	f.String("state", "", "state file for offset persistence")
	f.Duration("refresh", 5*time.Minute, "topic refresh interval")
	f.Duration("timeout", 30*time.Second, "connection timeout")
	f.Duration("stale", 3*time.Minute, "reconnect if no data for this long (0 to disable)")
	f.Bool("retry", true, "retry connection on errors")
	f.Int("retry-max", 0, "maximum number of retries (0 means unlimited)")
	f.Bool("keep-aspath", false, "keep RouteViews collector ASN 6447 in AS_PATH")

	return s
}

func (s *RvLive) Attach() error {
	k := s.K
	s.broker = k.String("broker")
	s.topics = k.String("topics")
	s.group = k.String("group")
	s.stateFile = k.String("state")
	s.refresh = k.Duration("refresh")
	s.timeout = k.Duration("timeout")
	s.stale = k.Duration("stale")
	s.retry = k.Bool("retry")
	s.retryMax = k.Int("retry-max")
	s.keepAspath = k.Bool("keep-aspath")

	// override topics if --router is set
	if r := k.String("router"); r != "" {
		if regexp.MustCompile(`\.[0-9]+$`).MatchString(r) {
			s.topics = fmt.Sprintf(`^routeviews\.%s\.bmp_raw$`, regexp.QuoteMeta(r))
		} else {
			s.topics = fmt.Sprintf(`^routeviews\.%s\.[0-9]+\.bmp_raw$`, regexp.QuoteMeta(r))
		}
		s.Debug().Str("topic", s.topics).Msg("using router-specific topic")
	}

	// Compile topic regex
	var err error
	s.topicsRe, err = regexp.Compile(s.topics)
	if err != nil {
		return fmt.Errorf("invalid --topics regex: %w", err)
	}

	// Generate group ID if not provided
	if s.group == "" {
		s.group = fmt.Sprintf("bgpipe-%d-%d", os.Getpid(), time.Now().UnixNano())
	}

	// Initialize parsers
	s.obmpMsg = bmp.NewOpenBmp()
	s.bmpMsg = bmp.NewBmp()

	// Load state file if specified
	if s.stateFile != "" {
		s.Debug().Str("file", s.stateFile).Msg("loading state file")
		s.state, err = s.loadState()
		if err != nil {
			s.Warn().Err(err).Msg("failed to load state file, starting fresh")
			s.state = &rvState{Version: 1, Offsets: make(map[string]map[int32]int64)}
		} else {
			s.Debug().Int("topics", len(s.state.Offsets)).Time("updated_at", s.state.UpdatedAt).Msg("loaded state")
		}
	} else {
		s.state = &rvState{Version: 1, Offsets: make(map[string]map[int32]int64)}
	}

	s.in = s.P.AddInput(s.Dir)
	return nil
}

func (s *RvLive) Run() error {
	defer s.in.Close()

	lastTry := time.Now()
	for try := 1; s.Ctx.Err() == nil; try++ {
		// Reset try count after long success
		if time.Since(lastTry) > time.Hour {
			try = 1
		}
		if s.retryMax > 0 && try > s.retryMax {
			return fmt.Errorf("max retries (%d) exceeded", s.retryMax)
		}
		lastTry = time.Now()

		// Backoff before retry (skip on first try)
		if try > 1 {
			sec := min(60, (try-1)*(try-1)) + rand.Intn(try)
			s.Info().Int("try", try).Int("wait_sec", sec).Msg("waiting before reconnect")
			select {
			case <-time.After(time.Duration(sec) * time.Second):
			case <-s.Ctx.Done():
				return context.Cause(s.Ctx)
			}
		}

		// Run Kafka consumer
		err := s.runKafka()
		if err == nil || s.Ctx.Err() != nil {
			return err
		}

		if !s.retry {
			return fmt.Errorf("kafka error: %w", err)
		}
		s.Warn().Err(err).Msg("kafka connection ended")
	}

	return context.Cause(s.Ctx)
}

func (s *RvLive) Stop() error {
	s.in.Close()
	return nil
}
