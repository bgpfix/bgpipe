package rvlive

import (
	"testing"
	"time"

	"github.com/bgpfix/bgpipe/core"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// a silent stream must end consume(), so that Run() can reconnect
func TestConsumeStale(t *testing.T) {
	client, err := kgo.NewClient(kgo.SeedBrokers("127.0.0.1:1"), kgo.ConsumeTopics("x"))
	require.NoError(t, err)
	defer client.Close()

	s := &RvLive{StageBase: &core.StageBase{Ctx: t.Context()}, stale: 100 * time.Millisecond}
	require.ErrorContains(t, s.consume(t.Context(), client), "connection stale")
}
