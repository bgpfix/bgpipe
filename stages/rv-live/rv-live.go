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

	broker        string
	topics        string
	topics_re     *regexp.Regexp
	collector     []string
	collector_not []string
	group         string
	state_file    string
	refresh       time.Duration
	timeout       time.Duration
	stale         time.Duration
	retry         bool
	retry_max     int
	keep_aspath   bool

	// state management
	state       *rvState
	state_mu    sync.Mutex
	state_dirty bool

	// reusable parsers
	obmp_msg *bmp.OpenBmp
	bmp_msg  *bmp.Bmp
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
	f.StringSlice("collector", nil, "collector name must start with this prefix (on top of topics regex)")
	f.StringSlice("collector-not", nil, "collector name must not start with this prefix (on top of topics regex)")
	f.String("group", "", "consumer group ID (auto-generated if empty)")
	f.String("state", "", "state file for offset persistence")
	f.Duration("refresh", 5*time.Minute, "topic refresh interval")
	f.Duration("timeout", 30*time.Second, "connection timeout")
	f.Duration("stale", 3*time.Minute, "reconnect if no data for this long (0 to disable)")
	f.Bool("retry", true, "retry connection on errors")
	f.Int("retry-max", 0, "maximum number of retries (0 means unlimited)")
	f.Bool("keep-aspath", false, "keep collector AS in AS_PATH")

	return s
}

func (s *RvLive) Attach() error {
	k := s.K
	s.broker = k.String("broker")
	s.topics = k.String("topics")
	s.group = k.String("group")
	s.state_file = k.String("state")
	s.refresh = k.Duration("refresh")
	s.timeout = k.Duration("timeout")
	s.stale = k.Duration("stale")
	s.retry = k.Bool("retry")
	s.retry_max = k.Int("retry-max")
	s.keep_aspath = k.Bool("keep-aspath")

	// parse --collector and --collector-not
	for _, r := range k.Strings("collector") {
		s.collector = append(s.collector, "routeviews."+r)
	}
	for _, r := range k.Strings("collector-not") {
		s.collector_not = append(s.collector_not, "routeviews."+r)
	}

	// Compile topic regex
	var err error
	s.topics_re, err = regexp.Compile(s.topics)
	if err != nil {
		return fmt.Errorf("invalid --topics regex: %w", err)
	}

	// Generate group ID if not provided
	if s.group == "" {
		s.group = fmt.Sprintf("bgpipe-%d-%d", os.Getpid(), time.Now().UnixNano())
	}

	// Initialize parsers
	s.obmp_msg = bmp.NewOpenBmp()
	s.bmp_msg = bmp.NewBmp()

	// Load state file if specified
	if s.state_file != "" {
		s.Debug().Str("file", s.state_file).Msg("loading state file")
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
		if s.retry_max > 0 && try > s.retry_max {
			return fmt.Errorf("max retries (%d) exceeded", s.retry_max)
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
