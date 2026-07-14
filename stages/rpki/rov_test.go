package rpki

import (
	"net/netip"
	"testing"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/meta"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/stretchr/testify/require"
)

func TestRov_WithdrawsOnlyInvalidPrefixes(t *testing.T) {
	s := newTestRov()
	s.tag = true
	s.act = act_withdraw

	s.cache.AddVRP(true, netip.MustParsePrefix("192.0.2.0/24"), 24, 65001)
	s.cache.AddVRP(true, netip.MustParsePrefix("198.51.100.0/24"), 24, 65002)
	s.cache.Apply()

	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24", "198.51.100.0/24")
	setAsPathSeq(m, 65001)

	require.True(t, s.validateMsg(m))
	require.Equal(t, []string{"192.0.2.0/24"}, prefixStrings(m.Update.AllReach()))
	require.Equal(t, []string{"198.51.100.0/24"}, prefixStrings(m.Update.AllUnreach()))
	require.Equal(t, "INVALID", pipe.UseTags(m)["rov/status"])
}

func TestRov_PureIPv6WithdrawalDropsPathAttrs(t *testing.T) {
	s := newTestRov()
	s.act = act_withdraw

	s.cache.AddVRP(true, netip.MustParsePrefix("2001:db8::/48"), 48, 65002)
	s.cache.Apply()

	m := newReachUpdate(meta.DIR_R, "2001:db8::/48")
	setAsPathSeq(m, 65001)

	require.True(t, s.validateMsg(m))
	require.False(t, m.Update.HasReach())
	require.Equal(t, []string{"2001:db8::/48"}, prefixStrings(m.Update.AllUnreach()))
	require.True(t, m.Update.Attrs.Has(attrs.ATTR_MP_UNREACH))
	require.False(t, m.Update.Attrs.Has(attrs.ATTR_MP_REACH))
	require.False(t, m.Update.Attrs.Has(attrs.ATTR_ASPATH))
}

func TestRov_StrictTreatsNotFoundAsInvalid(t *testing.T) {
	s := newTestRov()
	s.act = act_withdraw
	s.strict = true

	// non-empty cache without a covering VRP
	s.cache.AddVRP(true, netip.MustParsePrefix("192.0.2.0/24"), 24, 65001)
	s.cache.Apply()

	m := newReachUpdate(meta.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65001)

	require.True(t, s.validateMsg(m))
	require.False(t, m.Update.HasReach())
	require.Equal(t, []string{"10.0.0.0/8"}, prefixStrings(m.Update.AllUnreach()))
}

func TestRov_KeepTagsOnly(t *testing.T) {
	s := newTestRov()
	s.tag = true
	s.act = act_keep

	s.cache.AddVRP(true, netip.MustParsePrefix("192.0.2.0/24"), 24, 65002)
	s.cache.Apply()

	m := newReachUpdate(meta.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65001) // wrong origin -> INVALID

	require.True(t, s.validateMsg(m))
	require.Equal(t, []string{"192.0.2.0/24"}, prefixStrings(m.Update.AllReach()))
	require.False(t, m.Update.HasUnreach())
	tags := pipe.UseTags(m)
	require.Equal(t, "INVALID", tags["rov/status"])
	require.Equal(t, "INVALID", tags["rov/192.0.2.0/24"])
}
