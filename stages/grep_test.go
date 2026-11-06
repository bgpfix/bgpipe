package stages

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bgpfix/bgpfix/filter"
	"github.com/bgpfix/bgpfix/meta"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
)

// the L and R direction goroutines call the same Grep.check: no shared filter state
func TestGrepBothDirections(t *testing.T) {
	p := pipe.NewPipe(context.Background())
	f, err := filter.NewFilter("prefix ~ 10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	s := &Grep{filter: f, eval: [2]*filter.Eval{filter.NewEval(false), filter.NewEval(false)}}

	mk := func(d meta.Dir, pfx string) *msg.Msg {
		m := msg.NewMsg().Switch(msg.UPDATE)
		m.Dir = d
		pipe.UseContext(m).Pipe = p
		m.Update.AddReach(nlri.FromPrefix(netip.MustParsePrefix(pfx)))
		return m
	}

	var wrong atomic.Int64
	var wg sync.WaitGroup
	run := func(d meta.Dir, pfx string, want bool) {
		defer wg.Done()
		m := mk(d, pfx)
		for range 20000 {
			if s.check(m) != want {
				wrong.Add(1)
			}
		}
	}
	wg.Add(2)
	go run(meta.DIR_R, "10.1.0.0/16", true)   // match: kept
	go run(meta.DIR_L, "192.0.2.0/24", false) // no match: dropped
	wg.Wait()

	if n := wrong.Load(); n != 0 {
		t.Errorf("%d wrong verdicts", n)
	}
}
