//go:build !race

// NB: excluded from -race because of the shutdown race in bgpfix Pipe.Stop()

package stages

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bgpfix/bgpfix/meta"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// keepalive returns a RIS Live message with a BGP KEEPALIVE and given timestamp
func keepalive(ts time.Time) string {
	return fmt.Sprintf(`{"type":"ris_message","data":{"timestamp":%d.0,"peer":"192.0.2.1","peer_asn":"65001",`+
		`"id":"x","host":"rrc00.ripe.net","type":"KEEPALIVE","raw":"FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF001304"}}`+"\n", ts.Unix())
}

func newTestRis(t *testing.T) *RisLive {
	p := pipe.NewPipe(t.Context())
	p.Options.Logger = nil
	s := &RisLive{StageBase: &core.StageBase{Logger: zerolog.New(io.Discard), Ctx: t.Context()}}
	s.P, s.Dir = p, meta.DIR_R
	s.timeout_read, s.delay_err = time.Minute, 3*time.Minute
	s.in = p.AddInput(s.Dir)
	require.NoError(t, p.Start())
	go func() {
		for m := range p.R.Out {
			p.PutMsg(m)
		}
	}()
	return s
}

func streamBody(t *testing.T, s *RisLive, body string) error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) }))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	return s.stream(resp)
}

// an old message from a backlogged peer must not end the stream, even as the first one
func TestRisLiveOldFirstMessage(t *testing.T) {
	s := newTestRis(t)
	require.NoError(t, streamBody(t, s, keepalive(time.Now().Add(-500*time.Hour))))
}

// a stream that really falls behind must still end
func TestRisLiveFallsBehind(t *testing.T) {
	s := newTestRis(t)
	s.delay_err = time.Second
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		time.Sleep(2100 * time.Millisecond)
		io.WriteString(w, keepalive(time.Now().Add(-1100*time.Millisecond)))
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	require.ErrorContains(t, s.stream(resp), "delay too high")
}

// any connection error must be retried with --retry, not end the pipeline
func TestRisLiveRetriesConnectErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused

	s := newTestRis(t)
	s.url, s.retry, s.retry_max = url, true, 1
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	s.Ctx = ctx
	require.ErrorContains(t, s.Run(), "max retries")
}
