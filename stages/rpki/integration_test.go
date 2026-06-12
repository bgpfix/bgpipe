// NB: excluded from -race because Pipe.Stop() has an inherent race between
// go p.sendEvent(EVENT_STOP) and close(p.evch). The recover() in sendEvent
// makes this safe at runtime, but the race detector flags it.
//
//go:build !race

package rpki

import (
	"testing"
	"time"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/stretchr/testify/require"
)

// NB: end-to-end coverage for: cache load → pipe callback wiring →
// UPDATE validation → ROV/ASPA actions. Unit tests in rov_test.go and
// aspa_test.go call validateMsg directly and do not exercise actual
// pipe flow or rov+aspa chaining.

const integrationFixture = `{
	"roas": [
		{"prefix": "192.0.2.0/24",    "maxLength": 24, "asn": 65001},
		{"prefix": "198.51.100.0/24", "maxLength": 24, "asn": 65002},
		{"prefix": "203.0.113.0/24",  "maxLength": 24, "asn": 65020}
	],
	"aspas": [
		{"customer_asid": 65010, "provider_asids": [65001]},
		{"customer_asid": 65020, "provider_asids": [65010]},
		{"customer_asid": 65099, "provider_asids": [65030]}
	]
}`

// newIntegrationRov returns a Rov stage with the fixture loaded and applied.
func newIntegrationRov(t *testing.T) *Rov {
	t.Helper()
	s := newTestRov()
	require.NoError(t, s.cache.Parse([]byte(integrationFixture)))
	s.cache.Apply()
	return s
}

// addIntegrationAspa returns an Aspa stage sharing the pipe and cache with rov.
func addIntegrationAspa(rov *Rov) *Aspa {
	s := newTestAspa()
	s.P = rov.P
	s.cache = rov.cache
	return s
}

// startPipe wires the given UPDATE callbacks into the pipe and starts it.
// Pipe is stopped automatically at test teardown.
func startPipe(t *testing.T, p *pipe.Pipe, cbs ...pipe.CallbackFunc) {
	t.Helper()
	for _, cb := range cbs {
		p.Options.OnMsg(cb, dir.DIR_R, msg.UPDATE)
	}
	require.NoError(t, p.Start())
	t.Cleanup(func() { p.Stop() })
}

// sendUpdate writes m to R and returns the processed message from R.Out,
// or nil if dropped by a callback within timeout.
func sendUpdate(t *testing.T, p *pipe.Pipe, m *msg.Msg, timeout time.Duration) *msg.Msg {
	t.Helper()
	require.NoError(t, p.R.WriteMsg(m))
	select {
	case out := <-p.R.Out:
		return out
	case <-time.After(timeout):
		return nil
	}
}

func TestIntegration_CacheLoadedFromFixture(t *testing.T) {
	s := newIntegrationRov(t)
	vrps4, _, aspas := s.cache.Sizes()
	require.Equal(t, 3, vrps4)
	require.Equal(t, 3, aspas)
	// NB: AddASPA sorts providers for BinarySearch
	require.Equal(t, []uint32{65010}, s.cache.ASPAs()[65020])
}

func TestIntegration_ROV_WithdrawMovesInvalidToUnreach(t *testing.T) {
	s := newIntegrationRov(t)
	s.act = act_withdraw
	s.tag = true
	startPipe(t, s.P, s.validateMsg)

	// 192.0.2.0/24 VALID (origin 65001 matches VRP), 198.51.100.0/24
	// INVALID (origin 65001 but VRP wants 65002)
	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24", "198.51.100.0/24")
	setAsPathSeq(m, 65001)

	got := sendUpdate(t, s.P, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, []string{"192.0.2.0/24"}, prefixStrings(got.Update.AllReach()))
	require.Equal(t, []string{"198.51.100.0/24"}, prefixStrings(got.Update.AllUnreach()))
	require.Equal(t, "INVALID", pipe.UseTags(got)["rov/status"])
}

func TestIntegration_ROV_DropDiscardsMessage(t *testing.T) {
	s := newIntegrationRov(t)
	s.act = act_drop
	s.tag = true
	startPipe(t, s.P, s.validateMsg)

	m := newReachUpdate(dir.DIR_R, "198.51.100.0/24")
	setAsPathSeq(m, 65099) // no VRP with origin 65099

	require.Nil(t, sendUpdate(t, s.P, m, 200*time.Millisecond))
}

func TestIntegration_ROV_NotFoundPassesThrough(t *testing.T) {
	s := newIntegrationRov(t)
	s.act = act_withdraw
	s.tag = true
	startPipe(t, s.P, s.validateMsg)

	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65001)

	got := sendUpdate(t, s.P, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "NOT_FOUND", pipe.UseTags(got)["rov/status"])
	require.Equal(t, []string{"10.0.0.0/8"}, prefixStrings(got.Update.AllReach()))
}

func TestIntegration_ASPA_ValidPathPassesThrough(t *testing.T) {
	rov := newIntegrationRov(t)
	s := addIntegrationAspa(rov)
	s.role, _ = parseRoleName("customer")
	s.act = act_withdraw
	s.tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s.P, s.validateMsg)

	// path [65010, 65020]: peer matches path[0]; 65020→{65010} is attested
	m := newReachUpdate(dir.DIR_R, "203.0.113.0/24")
	setAsPathSeq(m, 65010, 65020)

	got := sendUpdate(t, s.P, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "VALID", pipe.UseTags(got)["aspa/status"])
	require.Equal(t, []string{"203.0.113.0/24"}, prefixStrings(got.Update.AllReach()))
}

func TestIntegration_ASPA_InvalidPath_WithdrawStripsReach(t *testing.T) {
	rov := newIntegrationRov(t)
	s := addIntegrationAspa(rov)
	s.role, _ = parseRoleName("customer")
	s.act = act_withdraw
	s.tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s.P, s.validateMsg)

	// path [65010, 65099]: 65099 has ASPA={65030}, 65010 not listed → INVALID
	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65010, 65099)

	got := sendUpdate(t, s.P, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "INVALID", pipe.UseTags(got)["aspa/status"])
	require.False(t, got.Update.HasReach())
	require.Equal(t, []string{"10.0.0.0/8"}, prefixStrings(got.Update.AllUnreach()))
	// NB: RFC 4271 §4.3 - pure withdrawal must not carry path attributes
	require.False(t, got.Update.Attrs.Has(attrs.ATTR_MP_REACH))
}

func TestIntegration_ASPA_InvalidPath_DropDiscardsMessage(t *testing.T) {
	rov := newIntegrationRov(t)
	s := addIntegrationAspa(rov)
	s.role, _ = parseRoleName("customer")
	s.act = act_drop
	s.tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s.P, s.validateMsg)

	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65010, 65099)

	require.Nil(t, sendUpdate(t, s.P, m, 200*time.Millisecond))
}

func TestIntegration_ASPA_InvalidPath_KeepTagsOnly(t *testing.T) {
	rov := newIntegrationRov(t)
	s := addIntegrationAspa(rov)
	s.role, _ = parseRoleName("customer")
	s.act = act_keep
	s.tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s.P, s.validateMsg)

	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65010, 65099)

	got := sendUpdate(t, s.P, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "INVALID", pipe.UseTags(got)["aspa/status"])
	require.Equal(t, []string{"10.0.0.0/8"}, prefixStrings(got.Update.AllReach()))
	require.False(t, got.Update.HasUnreach())
}

func TestIntegration_ASPA_UnknownPathPassesThrough(t *testing.T) {
	rov := newIntegrationRov(t)
	s := addIntegrationAspa(rov)
	s.role, _ = parseRoleName("customer")
	s.act = act_withdraw
	s.tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s.P, s.validateMsg)

	// 65040 has no ASPA record → every hop is "no attestation" → UNKNOWN
	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65010, 65040)

	got := sendUpdate(t, s.P, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "UNKNOWN", pipe.UseTags(got)["aspa/status"])
	require.Equal(t, []string{"10.0.0.0/8"}, prefixStrings(got.Update.AllReach()))
}

func TestIntegration_ASPA_FirstHopMismatchIsInvalid(t *testing.T) {
	rov := newIntegrationRov(t)
	s := addIntegrationAspa(rov)
	s.role, _ = parseRoleName("customer")
	s.act = act_withdraw
	s.tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s.P, s.validateMsg)

	// path[0]=65099 != peer 65010 → INVALID regardless of ASPA content
	m := newReachUpdate(dir.DIR_R, "203.0.113.0/24")
	setAsPathSeq(m, 65099, 65020)

	got := sendUpdate(t, s.P, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "INVALID", pipe.UseTags(got)["aspa/status"])
	require.False(t, got.Update.HasReach())
}

func TestIntegration_ROVValid_ASPAInvalid_BothActionsFire(t *testing.T) {
	rov := newIntegrationRov(t)
	rov.act = act_withdraw
	rov.tag = true

	aspa := addIntegrationAspa(rov)
	aspa.role, _ = parseRoleName("customer")
	aspa.act = act_withdraw
	aspa.tag = true

	storeOpenASN(rov.P, dir.DIR_R, 65010)
	startPipe(t, rov.P, rov.validateMsg, aspa.validateMsg)

	// 192.0.2.0/24 origin 65001 → ROV VALID; path breaks ASPA at 65099
	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65099, 65001)

	got := sendUpdate(t, rov.P, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "VALID", pipe.UseTags(got)["rov/status"])
	require.Equal(t, "INVALID", pipe.UseTags(got)["aspa/status"])
	require.False(t, got.Update.HasReach())
	require.Equal(t, []string{"192.0.2.0/24"}, prefixStrings(got.Update.AllUnreach()))
}
