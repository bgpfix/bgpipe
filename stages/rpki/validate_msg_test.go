package rpki

import (
	"context"
	"net/netip"
	"testing"

	"github.com/VictoriaMetrics/metrics"
	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/stretchr/testify/require"
)

func newValidateTestRpki() *Rpki {
	s := newTestRpki()
	s.P = pipe.NewPipe(context.Background())

	v4 := VRPs{}
	v6 := VRPs{}
	a := ASPA{}
	s.vrp4.Store(&v4)
	s.vrp6.Store(&v6)
	s.aspa.Store(&a)

	s.cnt_msg = metrics.GetOrCreateCounter("test_bgpipe_rpki_messages_total")
	s.cnt_rov_valid = metrics.GetOrCreateCounter("test_bgpipe_rpki_rov_valid_total")
	s.cnt_rov_inv = metrics.GetOrCreateCounter("test_bgpipe_rpki_rov_invalid_total")
	s.cnt_rov_nf = metrics.GetOrCreateCounter("test_bgpipe_rpki_rov_not_found_total")
	s.cnt_aspa_valid = metrics.GetOrCreateCounter("test_bgpipe_rpki_aspa_valid_total")
	s.cnt_aspa_unk = metrics.GetOrCreateCounter("test_bgpipe_rpki_aspa_unknown_total")
	s.cnt_aspa_inv = metrics.GetOrCreateCounter("test_bgpipe_rpki_aspa_invalid_total")

	return s
}

func newReachUpdate(dst dir.Dir, prefixes ...string) *msg.Msg {
	m := msg.NewMsg()
	m.Switch(msg.UPDATE)
	m.Dir = dst
	for _, prefix := range prefixes {
		m.Update.AddReach(nlri.FromPrefix(netip.MustParsePrefix(prefix)))
	}
	return m
}

func setAsPathSeq(m *msg.Msg, asns ...uint32) {
	ap := m.Update.Attrs.Use(attrs.ATTR_ASPATH).(*attrs.Aspath)
	ap.Segments = append(ap.Segments[:0], attrs.AspathSegment{List: asns})
}

func setEmptyAsPath(m *msg.Msg) {
	ap := m.Update.Attrs.Use(attrs.ATTR_ASPATH).(*attrs.Aspath)
	ap.Segments = ap.Segments[:0]
}

func storeOpenASN(p *pipe.Pipe, d dir.Dir, asn int) {
	om := &msg.Open{}
	om.Caps.Init()
	om.SetASN(asn)
	p.LineFor(d).Open.Store(om)
}

func prefixStrings(pp []nlri.Prefix) []string {
	out := make([]string, 0, len(pp))
	for _, prefix := range pp {
		out = append(out, prefix.String())
	}
	return out
}

func TestValidateMsg_WithdrawsOnlyInvalidPrefixes(t *testing.T) {
	s := newValidateTestRpki()
	s.tag = true
	s.rov_act = act_withdraw

	v4 := VRPs{
		netip.MustParsePrefix("192.0.2.0/24"): {
			{MaxLen: 24, ASN: 65001},
		},
		netip.MustParsePrefix("198.51.100.0/24"): {
			{MaxLen: 24, ASN: 65002},
		},
	}
	s.vrp4.Store(&v4)

	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24", "198.51.100.0/24")
	setAsPathSeq(m, 65001)

	require.True(t, s.validateMsg(m))
	require.Equal(t, []string{"192.0.2.0/24"}, prefixStrings(m.Update.AllReach()))
	require.Equal(t, []string{"198.51.100.0/24"}, prefixStrings(m.Update.AllUnreach()))
	require.Equal(t, "INVALID", pipe.UseTags(m)["rpki/status"])
}

func TestValidateMsg_PureIPv6WithdrawalDropsPathAttrs(t *testing.T) {
	s := newValidateTestRpki()
	s.rov_act = act_withdraw

	v6 := VRPs{
		netip.MustParsePrefix("2001:db8::/48"): {
			{MaxLen: 48, ASN: 65002},
		},
	}
	s.vrp6.Store(&v6)

	m := newReachUpdate(dir.DIR_R, "2001:db8::/48")
	setAsPathSeq(m, 65001)

	require.True(t, s.validateMsg(m))
	require.False(t, m.Update.HasReach())
	require.Equal(t, []string{"2001:db8::/48"}, prefixStrings(m.Update.AllUnreach()))
	require.True(t, m.Update.Attrs.Has(attrs.ATTR_MP_UNREACH))
	require.False(t, m.Update.Attrs.Has(attrs.ATTR_MP_REACH))
	require.False(t, m.Update.Attrs.Has(attrs.ATTR_ASPATH))
}

func TestValidateMsg_AspaEmptyASPathIsSkipped(t *testing.T) {
	s := newValidateTestRpki()
	s.aspa_on = true
	s.aspa_role = "customer"
	s.aspa_act = act_drop

	a := ASPA{
		65020: {65010},
	}
	s.aspa.Store(&a)

	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setEmptyAsPath(m)

	require.NotPanics(t, func() {
		require.True(t, s.validateMsg(m))
	})
}

func TestValidateMsg_AspaFirstHopMismatchDrops(t *testing.T) {
	s := newValidateTestRpki()
	s.aspa_on = true
	s.aspa_role = "customer"
	s.aspa_act = act_drop

	a := ASPA{
		65020: {65010},
	}
	s.aspa.Store(&a)
	storeOpenASN(s.P, dir.DIR_R, 65099)

	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)

	require.False(t, s.validateMsg(m))
}

func TestValidateMsg_AspaRouteServerSkipsFirstHopCheck(t *testing.T) {
	s := newValidateTestRpki()
	s.aspa_on = true
	s.aspa_role = "rs"
	s.aspa_act = act_drop

	a := ASPA{
		65020: {65010},
	}
	s.aspa.Store(&a)
	storeOpenASN(s.P, dir.DIR_R, 65099)

	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65020)

	require.True(t, s.validateMsg(m))
}
