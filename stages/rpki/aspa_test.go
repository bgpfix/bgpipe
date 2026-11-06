package rpki

import (
	"testing"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/meta"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/stretchr/testify/require"
)

func TestParseRoleName(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"provider", true},
		{"Provider", true},
		{"PROVIDER", true},
		{"rs", true},
		{"RS", true},
		{"rs-client", true},
		{"customer", true},
		{"peer", true},
		{"unknown", false},
		{"auto", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := parseRoleName(tc.name)
			require.Equal(t, tc.ok, ok)
		})
	}
}

func TestAspa_ResolvePeerRoles(t *testing.T) {
	tests := []struct {
		role string
		down bool
		rs   bool
	}{
		{"provider", true, false},
		{"rs", false, true},
		{"rs-client", false, false},
		{"customer", false, false},
		{"peer", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.role, func(t *testing.T) {
			s := newTestAspa()
			var ok bool
			s.role, ok = parseRoleName(tc.role)
			require.True(t, ok)
			p := s.resolvePeer(meta.DIR_R)
			require.True(t, p.ok)
			require.Equal(t, tc.down, p.down)
			require.Equal(t, tc.rs, p.rs)
		})
	}
}

func TestAspa_ResolvePeerAutoNoCapability(t *testing.T) {
	s := newTestAspa()
	s.role_auto = true

	// no OPEN message stored -> role unknown -> ASPA skipped
	p := s.resolvePeer(meta.DIR_R)
	require.False(t, p.ok)
}

// an announcement with an empty AS_PATH is Invalid (draft-29 sections 5.5/5.6)
func TestAspa_EmptyASPathIsInvalid(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("customer")
	s.act = act_drop

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()

	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setEmptyAsPath(m)

	require.NotPanics(t, func() {
		require.False(t, s.validateMsg(m))
	})
}

func TestAspa_FirstHopMismatchDrops(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("customer")
	s.act = act_drop

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()
	storeOpenASN(s.P, meta.DIR_R, 65099)

	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)

	require.False(t, s.validateMsg(m))
}

func TestAspa_RouteServerSkipsFirstHopCheck(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("rs")
	s.act = act_drop

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()
	storeOpenASN(s.P, meta.DIR_R, 65099)

	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)

	require.True(t, s.validateMsg(m))
}

func TestAspa_FirstHopDisabledSkipsCheck(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("customer")
	s.act = act_drop
	s.first_hop = false // collector-feed config

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()
	storeOpenASN(s.P, meta.DIR_R, 65099) // peer ASN differs from path[0]

	// path[0]=65010 != neighbor 65099, but the check is off, so the path is
	// verified on its merits (65010 -> 65020 attested) and kept
	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)

	require.True(t, s.validateMsg(m))
}

func TestAspa_PeerTagFirstHopCheck(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("peer")
	s.act = act_drop
	s.peer_tag = "PEER_AS"

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()

	// matching tag -> first-hop OK, path attested -> kept
	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)
	pipe.UseTags(m)["PEER_AS"] = "65010"
	require.True(t, s.validateMsg(m))

	// mismatching tag -> first-hop check fails -> dropped
	m2 := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m2, 65010, 65020)
	pipe.UseTags(m2)["PEER_AS"] = "65099"
	require.False(t, s.validateMsg(m2))

	// missing tag -> first-hop check disabled -> kept
	m3 := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m3, 65010, 65020)
	require.True(t, s.validateMsg(m3))
}

func TestAspa_PeerASNCachedPerDirection(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("customer")
	s.act = act_drop

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()
	storeOpenASN(s.P, meta.DIR_R, 65010)

	// first UPDATE resolves peer ASN 65010 -> first-hop check passes
	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)
	require.True(t, s.validateMsg(m))

	// NB: peer state is resolved once; a changed OPEN is ignored
	storeOpenASN(s.P, meta.DIR_R, 65099)
	m2 := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m2, 65010, 65020)
	require.True(t, s.validateMsg(m2))
}

func TestAspa_SingleHopPathChecksFirstHop(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("customer")
	s.act = act_drop

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()
	storeOpenASN(s.P, meta.DIR_R, 65099)

	// the peer omits its own ASN, path[0] is not the neighbor
	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65020)
	require.False(t, s.validateMsg(m))

	m = newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65099)
	require.True(t, s.validateMsg(m))
}

func TestAspa_WithdrawStripsPathAttrs(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("customer")
	s.act = act_withdraw

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()
	storeOpenASN(s.P, meta.DIR_R, 65099)

	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24", "2001:db8::/32")
	setAsPathSeq(m, 65010, 65020) // first hop is not 65099
	u := &m.Update
	require.True(t, u.HasReach()) // fills the AllReach cache

	require.True(t, s.validateMsg(m))
	require.False(t, u.HasReach())
	require.ElementsMatch(t, []string{"192.0.2.0/24", "2001:db8::/32"}, prefixStrings(u.AllUnreach()))
	require.Equal(t, []attrs.Code{attrs.ATTR_MP_UNREACH}, attrCodes(u))
}

func attrCodes(u *msg.Update) (cs []attrs.Code) {
	u.Attrs.Each(func(i int, ac attrs.Code, at attrs.Attr) { cs = append(cs, ac) })
	return
}
