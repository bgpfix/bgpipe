package stages

import (
	"context"
	"fmt"
	"net/netip"
	"testing"

	"github.com/bgpfix/bgpfix/afi"
	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/puzpuzpuz/xsync/v4"
)

func newTestLimit() *Limit {
	base := &core.StageBase{P: pipe.NewPipe(context.Background())}
	base.P.Options.Logger = nil
	s := &Limit{StageBase: base, ipv4: true, ipv6: true, blen4: 16, blen6: 32, limit_origin: 2, limit_block: 2}
	s.afs = map[afi.AS]bool{afi.AS_IPV4_UNICAST: true, afi.AS_IPV6_UNICAST: true}
	s.session = xsync.NewMap[nlri.Prefix, *limitPrefix]()
	s.origin = xsync.NewMap[uint32, *limitCounter]()
	s.block = xsync.NewMap[uint64, *limitCounter]()
	return s
}

func limitUpdate(reach bool, p string, origin uint32) *msg.Msg {
	m := msg.NewMsg().Switch(msg.UPDATE)
	pf := nlri.FromPrefix(netip.MustParsePrefix(p))
	if reach {
		m.Update.AddReach(pf)
		ap := m.Update.Attrs.Use(attrs.ATTR_ASPATH).(*attrs.Aspath)
		ap.FromJSON(fmt.Appendf(nil, "[100,%d]", origin))
	} else {
		m.Update.AddUnreach(pf)
	}
	return m
}

// origin and block counters must not outlive the prefixes they count
func TestLimitCountersEvicted(t *testing.T) {
	s := newTestLimit()
	for i := 1; i <= 1000; i++ {
		v4, v6 := fmt.Sprintf("10.%d.%d.0/24", i>>8, i&255), fmt.Sprintf("%x:%x::/48", 0x2000, i)
		for _, p := range []string{v4, v6} {
			if !s.onMsg(limitUpdate(true, p, uint32(i))) {
				t.Fatalf("%s dropped", p)
			}
			s.onMsg(limitUpdate(false, p, 0))
		}
	}
	if n, o, b := s.session.Size(), s.origin.Size(), s.block.Size(); n+o+b != 0 {
		t.Errorf("leftovers: session=%d origin=%d block=%d", n, o, b)
	}

	// a prefix dropped by the block limit leaves no origin counter behind
	for i, p := range []string{"10.1.0.0/24", "10.1.1.0/24", "10.1.2.0/24"} {
		if ok := s.onMsg(limitUpdate(true, p, uint32(100+i))); ok != (i < 2) {
			t.Errorf("%s: kept=%v", p, ok)
		}
	}
	if o := s.origin.Size(); o != 2 {
		t.Errorf("origin counters=%d, want 2", o)
	}

	// limits still apply after eviction, and recover after a withdrawal
	s.onMsg(limitUpdate(false, "10.1.0.0/24", 0))
	if !s.onMsg(limitUpdate(true, "10.1.2.0/24", 102)) {
		t.Errorf("block slot not released")
	}
}
