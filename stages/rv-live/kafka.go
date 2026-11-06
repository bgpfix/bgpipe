package rvlive

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/bmp"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func (s *RvLive) runKafka() error {
	s.Info().Str("broker", s.broker).Str("group", s.group).Msg("connecting")

	// Build client options - based on libbgpstream/rdkafka configuration
	opts := []kgo.Opt{
		kgo.SeedBrokers(s.broker),
		kgo.ConsumerGroup(s.group),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
		kgo.DisableAutoCommit(),

		// Use roundrobin balancer for even partition distribution (like bgpstream)
		// Default "range" can cause uneven distribution with many partitions/topics
		kgo.Balancers(kgo.RoundRobinBalancer()),

		// Connection settings
		kgo.ConnIdleTimeout(5 * time.Minute), // Keep connections alive longer

		// Fetch settings - optimized for real-time streaming (like bgpstream)
		kgo.FetchMaxWait(100 * time.Millisecond), // Don't wait long - we want real-time data
		kgo.FetchMaxPartitionBytes(128 * 1024),   // 128KB max per partition (prevents slow consumer issues)

		// NB: a whole fetch must be read within about 10s, or the client drops the connection
		// and re-fetches even more data; the 50MB default never recovers on a slow link
		kgo.FetchMaxBytes(2 << 20),

		// Consumer group settings for large partition counts
		kgo.RebalanceTimeout(120 * time.Second), // Allow time for rebalancing 1800+ partitions

		// Log partition assignments
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
			count := 0
			for _, parts := range assigned {
				count += len(parts)
			}
			s.Info().Int("topics", len(assigned)).Int("partitions", count).Msg("partitions assigned")
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
			count := 0
			for _, parts := range revoked {
				count += len(parts)
			}
			s.Debug().Int("topics", len(revoked)).Int("partitions", count).Msg("partitions revoked")
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
			count := 0
			for _, parts := range lost {
				count += len(parts)
			}
			s.Warn().Int("topics", len(lost)).Int("partitions", count).Msg("partitions lost")
		}),
	}

	// Create client
	s.Debug().Msg("creating Kafka client")
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return fmt.Errorf("failed to create kafka client: %w", err)
	}
	defer client.Close()
	clients := []*kgo.Client{client}

	// Discover and subscribe to topics
	s.Debug().Str("pattern", s.topics).Msg("discovering topics")
	topics, err := s.discoverTopics(client)
	if err != nil {
		return fmt.Errorf("failed to discover topics: %w", err)
	} else if len(topics) == 0 {
		return fmt.Errorf("no matching topics found for pattern: %s", s.topics)
	} else {
		s.Debug().Strs("topics", topics).Msg("discovered topics")
	}

	// NB: over long distances a broker sends only about 1MB/s per connection, and a client
	// fetches over one connection per broker, so read in parallel to keep up with the feed
	for len(clients) < min(s.consumers, len(topics)) {
		c, err := kgo.NewClient(opts...)
		if err != nil {
			return fmt.Errorf("failed to create kafka client: %w", err)
		}
		defer c.Close()
		clients = append(clients, c)
	}

	s.Info().Int("count", len(topics)).Int("consumers", len(clients)).Msg("subscribing to topics")
	for _, c := range clients {
		c.AddConsumeTopics(topics...)

		// Seek to saved offsets if we have state
		if s.state_file != "" && len(s.state.Offsets) > 0 {
			if err := s.seekToSavedOffsets(c, topics); err != nil {
				s.Warn().Err(err).Msg("failed to seek to saved offsets")
			}
		}
	}

	// Start state saver goroutine
	stateSaverDone := make(chan struct{})
	if s.state_file != "" {
		go s.stateSaver(stateSaverDone)
	}

	// Start topic refresher goroutine
	refreshDone := make(chan struct{})
	go s.topicRefresher(clients, refreshDone)
	s.Debug().Dur("interval", s.refresh).Msg("started topic refresher")

	// Consume messages, until the first consumer ends
	s.last_data.Store(time.Now().UnixNano())
	ctx, cancel := context.WithCancelCause(s.Ctx)
	defer cancel(nil)
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Go(func() { cancel(s.consume(ctx, c)) })
	}
	wg.Wait()
	if err = context.Cause(ctx); s.Ctx.Err() != nil || err == context.Canceled {
		err = nil
	}

	// Cleanup
	close(refreshDone)
	if s.state_file != "" {
		close(stateSaverDone)
		if err := s.saveState(); err != nil {
			s.Warn().Err(err).Msg("failed to do final save state")
		}
	}

	return err
}

func (s *RvLive) discoverTopics(client *kgo.Client) ([]string, error) {
	ctx, cancel := context.WithTimeout(s.Ctx, s.timeout)
	defer cancel()

	hasPrefix := func(name string, prefixes []string) bool {
		for _, prefix := range prefixes {
			if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
				return true
			}
		}
		return false
	}

	admin := kadm.NewClient(client)
	endOffsets, err := admin.ListEndOffsets(ctx)
	if err != nil {
		return nil, err
	}

	var topics []string
	for topic, partOffsets := range endOffsets {
		if !s.topics_re.MatchString(topic) {
			continue
		}

		// extract collector + router from topic name
		if len(s.collector) > 0 && !hasPrefix(topic, s.collector) {
			s.Trace().Str("topic", topic).Msg("skipping non-matching collector")
			continue
		}
		if len(s.collector_not) > 0 && hasPrefix(topic, s.collector_not) {
			s.Trace().Str("topic", topic).Msg("skipping excluded collector")
			continue
		}

		// skip dead topics (all partitions have offset 0)
		for _, po := range partOffsets {
			if po.Err == nil && po.Offset > 0 {
				s.Trace().Str("topic", topic).Msg("discovered matching topic")
				topics = append(topics, topic)
				break
			}
		}
	}

	return topics, nil
}

func (s *RvLive) seekToSavedOffsets(client *kgo.Client, topics []string) error {
	offsets := make(map[string]map[int32]kgo.EpochOffset)

	for _, topic := range topics {
		if partOffsets, ok := s.state.Offsets[topic]; ok {
			offsets[topic] = make(map[int32]kgo.EpochOffset)
			for part, off := range partOffsets {
				offsets[topic][part] = kgo.EpochOffset{Epoch: -1, Offset: off}
			}
		}
	}

	if len(offsets) > 0 {
		s.Debug().Interface("offsets", offsets).Msg("seeking to saved offsets")
		client.SetOffsets(offsets)
	}

	return nil
}

func (s *RvLive) topicRefresher(clients []*kgo.Client, done <-chan struct{}) {
	if s.refresh <= 0 {
		return // disabled
	}

	ticker := time.NewTicker(s.refresh)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-s.Ctx.Done():
			return
		case <-ticker.C:
			s.Debug().Msg("refreshing topic list")
			topics, err := s.discoverTopics(clients[0])
			if err != nil {
				s.Warn().Err(err).Msg("failed to refresh topics")
				continue
			}

			s.Debug().Int("count", len(topics)).Msg("topic refresh complete")
			// Add any new topics (franz-go handles duplicates)
			for _, c := range clients {
				c.AddConsumeTopics(topics...)
			}
		}
	}
}

func (s *RvLive) consume(ctx context.Context, client *kgo.Client) error {
	consecutiveErrors := 0
	om, bm := bmp.NewOpenBmp(), bmp.NewBmp()

	for ctx.Err() == nil {
		// NB: PollFetches blocks until there are records, so use a deadline to detect silence
		pctx, cancel := ctx, context.CancelFunc(func() {})
		if s.stale > 0 {
			pctx, cancel = context.WithTimeout(ctx, s.stale)
		}
		fetches := client.PollFetches(pctx)
		silent := pctx.Err() != nil && ctx.Err() == nil && fetches.NumRecords() == 0
		cancel()
		if silent {
			// NB: other consumers may still have data
			if d := time.Since(time.Unix(0, s.last_data.Load())); d >= s.stale {
				return fmt.Errorf("connection stale: no data for %v", d.Round(time.Second))
			}
			continue
		}

		// Handle errors
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				if e.Err == context.Canceled || e.Err == context.DeadlineExceeded {
					return nil
				}
				s.Warn().Err(e.Err).Str("topic", e.Topic).Int32("partition", e.Partition).Msg("fetch error")
			}
			consecutiveErrors++
			if consecutiveErrors >= 10 {
				return fmt.Errorf("too many consecutive fetch errors (%d)", consecutiveErrors)
			}
			continue
		}
		consecutiveErrors = 0

		// Process records
		if fetches.NumRecords() > 0 {
			s.last_data.Store(time.Now().UnixNano())
		}
		iter := fetches.RecordIter()
		for !iter.Done() && ctx.Err() == nil {
			record := iter.Next()

			if err := s.processRecord(record, om, bm); err != nil {
				s.Warn().Err(err).
					Str("topic", record.Topic).
					Int32("partition", record.Partition).
					Int64("offset", record.Offset).
					Msg("process error")
				continue
			}

			s.updateOffset(record.Topic, record.Partition, record.Offset+1)
		}
	}

	return nil
}
