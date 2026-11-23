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
	s := &Limit{StageBase: base, ipv4: true, ipv6: true, blen4: 16, blen6: 32, limit_origin: 2, limit_block: 2, counting: true}
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

// an UPDATE that announces and withdraws the same prefix must keep it counted (RFC 4271 4.3)
func TestLimitReachUnreachSamePrefix(t *testing.T) {
	pfx := func(p string) nlri.Prefix { return nlri.FromPrefix(netip.MustParsePrefix(p)) }

	// mp sets an IPv4 MP_REACH or MP_UNREACH attribute
	mp := func(m *msg.Msg, code attrs.Code, p nlri.Prefix, as ...afi.AS) {
		a := m.Update.Attrs.Use(code).(*attrs.MP)
		a.AS = afi.AS_IPV4_UNICAST
		if len(as) > 0 {
			a.AS = as[0]
		}
		v := attrs.NewMPPrefixes(a).(*attrs.MPPrefixes)
		if code == attrs.ATTR_MP_REACH {
			v.NextHop = netip.MustParseAddr("192.0.2.1")
		}
		v.Prefixes = append(v.Prefixes, p)
		a.Value = v
	}

	cases := []struct {
		name string
		p, q string
		make func(p nlri.Prefix) *msg.Msg
	}{
		{"ipv4", "10.0.0.0/24", "10.1.0.0/24", func(p nlri.Prefix) *msg.Msg {
			m := limitUpdate(true, p.String(), 65000)
			m.Update.AddUnreach(p)
			return m
		}},
		{"ipv6", "2001:db8::/48", "2001:db9::/48", func(p nlri.Prefix) *msg.Msg {
			m := limitUpdate(true, p.String(), 65000)
			m.Update.AddUnreach(p)
			return m
		}},
		{"mp-reach+withdrawn", "10.0.0.0/24", "10.1.0.0/24", func(p nlri.Prefix) *msg.Msg {
			m := limitUpdate(false, p.String(), 0)
			m.Update.Attrs.Use(attrs.ATTR_ASPATH).(*attrs.Aspath).FromJSON([]byte("[100,65000]"))
			mp(m, attrs.ATTR_MP_REACH, p)
			return m
		}},
		{"reach+mp-unreach", "10.0.0.0/24", "10.1.0.0/24", func(p nlri.Prefix) *msg.Msg {
			m := limitUpdate(true, p.String(), 65000)
			mp(m, attrs.ATTR_MP_UNREACH, p)
			return m
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestLimit()
			s.limit_session, s.limit_origin, s.limit_block = 1, 0, 0

			// P announced and withdrawn at once: announcement kept, withdrawal dropped
			m := tc.make(pfx(tc.p))
			if !s.onMsg(m) || len(m.Update.AllReach()) != 1 || len(m.Update.AllUnreach()) != 0 {
				t.Fatalf("P: reach=%v unreach=%v", m.Update.AllReach(), m.Update.AllUnreach())
			}
			if n := s.session.Size(); n != 1 {
				t.Errorf("P: session=%d, want 1", n)
			}

			// Q the same way: over the session limit, only the withdrawal goes through
			m = tc.make(pfx(tc.q))
			if !s.onMsg(m) || len(m.Update.AllReach()) != 0 || len(m.Update.AllUnreach()) != 1 {
				t.Errorf("Q: reach=%v unreach=%v", m.Update.AllReach(), m.Update.AllUnreach())
			}

			// a plain withdrawal still frees the slot
			s.onMsg(limitUpdate(false, tc.p, 0))
			if !s.onMsg(limitUpdate(true, tc.q, 65000)) {
				t.Errorf("Q: slot not released")
			}
		})
	}

	// origin and block counters stay in sync with an already accepted prefix
	s := newTestLimit()
	for range 3 {
		m := limitUpdate(true, "10.0.0.0/24", 65000)
		m.Update.AddUnreach(pfx("10.0.0.0/24"))
		s.onMsg(m)
	}
	if n, o, b := s.session.Size(), s.origin.Size(), s.block.Size(); n != 1 || o != 1 || b != 1 {
		t.Errorf("re-announced: session=%d origin=%d block=%d, want 1/1/1", n, o, b)
	}
	s.onMsg(limitUpdate(false, "10.0.0.0/24", 0))
	if n, o, b := s.session.Size(), s.origin.Size(), s.block.Size(); n+o+b != 0 {
		t.Errorf("withdrawn: session=%d origin=%d block=%d, want 0", n, o, b)
	}

	// an announcement in another address family does not cancel the withdrawal
	s = newTestLimit()
	s.onMsg(limitUpdate(true, "10.0.0.0/24", 65000))
	for _, inMP := range []bool{false, true} {
		m := limitUpdate(false, "10.0.0.0/24", 0)
		if inMP {
			m.Update.Unreach = nil
			mp(m, attrs.ATTR_MP_UNREACH, pfx("10.0.0.0/24"))
		}
		mp(m, attrs.ATTR_MP_REACH, pfx("10.0.0.0/24"), afi.AS_IPV4_MULTICAST)
		kept := s.onMsg(m)
		if !kept || len(m.Update.AllReach()) != 1 || len(m.Update.AllUnreach()) != 1 || s.session.Size() != 0 {
			t.Errorf("multicast announcement, unicast withdrawal (MP=%v): kept=%v reach=%v unreach=%v session=%d",
				inMP, kept, m.Update.AllReach(), m.Update.AllUnreach(), s.session.Size())
		}
		s.onMsg(limitUpdate(true, "10.0.0.0/24", 65000))
	}
}
