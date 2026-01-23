package rvlive

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func (s *RvLive) runKafka() error {
	s.Info().Str("broker", s.broker).Str("group", s.group).Msg("connecting")

	// Build client options
	opts := []kgo.Opt{
		kgo.SeedBrokers(s.broker),
		kgo.ConsumerGroup(s.group),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
		kgo.DisableAutoCommit(),
		kgo.ConnIdleTimeout(s.timeout),
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
	}

	// Create client
	s.Debug().Msg("creating Kafka client")
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return fmt.Errorf("failed to create kafka client: %w", err)
	}
	defer client.Close()

	// Discover and subscribe to topics
	s.Debug().Str("pattern", s.topics).Msg("discovering topics")
	topics, err := s.discoverTopics(client)
	if err != nil {
		return fmt.Errorf("failed to discover topics: %w", err)
	}
	if len(topics) == 0 {
		return fmt.Errorf("no matching topics found for pattern: %s", s.topics)
	}

	s.Info().Int("count", len(topics)).Msg("subscribing to topics")
	client.AddConsumeTopics(topics...)

	// Seek to saved offsets if we have state
	if s.stateFile != "" && len(s.state.Offsets) > 0 {
		if err := s.seekToSavedOffsets(client, topics); err != nil {
			s.Warn().Err(err).Msg("failed to seek to saved offsets")
		}
	}

	// Start state saver goroutine
	stateSaverDone := make(chan struct{})
	if s.stateFile != "" {
		go s.stateSaver(stateSaverDone)
	}

	// Start topic refresher goroutine
	refreshDone := make(chan struct{})
	go s.topicRefresher(client, refreshDone)
	s.Debug().Dur("interval", s.refresh).Msg("started topic refresher")

	// Consume messages
	err = s.consume(client)

	// Cleanup
	close(refreshDone)
	if s.stateFile != "" {
		close(stateSaverDone)
		s.saveState() // Final save
	}

	return err
}

func (s *RvLive) discoverTopics(client *kgo.Client) ([]string, error) {
	ctx, cancel := context.WithTimeout(s.Ctx, s.timeout)
	defer cancel()

	admin := kadm.NewClient(client)
	meta, err := admin.Metadata(ctx)
	if err != nil {
		return nil, err
	}

	var topics []string
	total := 0
	for _, t := range meta.Topics {
		if t.Err != nil {
			continue
		}
		total++
		if s.topicsRe.MatchString(t.Topic) {
			s.Debug().Str("topic", t.Topic).Msg("discovered matching topic")
			topics = append(topics, t.Topic)
		}
	}

	s.Debug().Int("total", total).Int("matching", len(topics)).Msg("discovered topics")
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

func (s *RvLive) topicRefresher(client *kgo.Client, done <-chan struct{}) {
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
			topics, err := s.discoverTopics(client)
			if err != nil {
				s.Warn().Err(err).Msg("failed to refresh topics")
				continue
			}

			s.Debug().Int("count", len(topics)).Msg("topic refresh complete")
			// Add any new topics (franz-go handles duplicates)
			client.AddConsumeTopics(topics...)
		}
	}
}

func (s *RvLive) consume(client *kgo.Client) error {
	trace := s.Trace().Enabled()

	for s.Ctx.Err() == nil {
		if trace {
			s.Trace().Msg("polling for fetches")
		}
		fetches := client.PollFetches(s.Ctx)
		if trace {
			s.Trace().Int("records", fetches.NumRecords()).Msg("fetched records")
		}

		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				if e.Err == context.Canceled || e.Err == context.DeadlineExceeded {
					return nil
				}
				s.Warn().Err(e.Err).Str("topic", e.Topic).Int32("partition", e.Partition).Msg("fetch error")
			}
			// Continue on non-fatal errors
			if s.Ctx.Err() != nil {
				return nil
			}
		}

		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()

			// Trace log raw Kafka data as hex (only if trace enabled)
			if trace {
				s.Trace().Str("topic", record.Topic).
					Int32("partition", record.Partition).
					Int64("offset", record.Offset).
					Int("len", len(record.Value)).
					Hex("data", record.Value).
					Msg("raw kafka record")
			}

			if err := s.processRecord(record); err != nil {
				s.Warn().Err(err).
					Str("topic", record.Topic).
					Int32("partition", record.Partition).
					Int64("offset", record.Offset).
					Msg("failed to process record")
				continue
			}

			// Update offset state
			s.updateOffset(record.Topic, record.Partition, record.Offset+1)
		}
	}

	return nil
}
