package rpki

import (
	"testing"

	"github.com/bgpfix/bgpfix/dir"
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
			p := s.resolvePeer(dir.DIR_R)
			require.True(t, p.ok)
			require.Equal(t, tc.down, p.down)
			require.Equal(t, tc.rs, p.rs)
		})
	}
}

func TestAspa_ResolvePeerAutoNoCapability(t *testing.T) {
	s := newTestAspa()
	s.role_auto = true

	// no OPEN message stored → role unknown → ASPA skipped
	p := s.resolvePeer(dir.DIR_R)
	require.False(t, p.ok)
}

func TestAspa_EmptyASPathIsSkipped(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("customer")
	s.act = act_drop

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()

	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setEmptyAsPath(m)

	require.NotPanics(t, func() {
		require.True(t, s.validateMsg(m))
	})
}

func TestAspa_FirstHopMismatchDrops(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("customer")
	s.act = act_drop

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()
	storeOpenASN(s.P, dir.DIR_R, 65099)

	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)

	require.False(t, s.validateMsg(m))
}

func TestAspa_RouteServerSkipsFirstHopCheck(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("rs")
	s.act = act_drop

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()
	storeOpenASN(s.P, dir.DIR_R, 65099)

	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
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

	// matching tag → first-hop OK, path attested → kept
	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)
	pipe.UseTags(m)["PEER_AS"] = "65010"
	require.True(t, s.validateMsg(m))

	// mismatching tag → first-hop check fails → dropped
	m2 := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m2, 65010, 65020)
	pipe.UseTags(m2)["PEER_AS"] = "65099"
	require.False(t, s.validateMsg(m2))

	// missing tag → first-hop check disabled → kept
	m3 := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m3, 65010, 65020)
	require.True(t, s.validateMsg(m3))
}

func TestAspa_PeerASNCachedPerDirection(t *testing.T) {
	s := newTestAspa()
	s.role, _ = parseRoleName("customer")
	s.act = act_drop

	s.cache.AddASPA(true, 65020, []uint32{65010})
	s.cache.Apply()
	storeOpenASN(s.P, dir.DIR_R, 65010)

	// first UPDATE resolves peer ASN 65010 → first-hop check passes
	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)
	require.True(t, s.validateMsg(m))

	// NB: peer state is resolved once; a changed OPEN is ignored
	storeOpenASN(s.P, dir.DIR_R, 65099)
	m2 := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m2, 65010, 65020)
	require.True(t, s.validateMsg(m2))
}
