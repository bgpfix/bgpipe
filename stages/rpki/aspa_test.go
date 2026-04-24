package rpki

import (
	"testing"

	"github.com/bgpfix/bgpfix/caps"
	"github.com/stretchr/testify/require"
)

// --- aspAuthorized tests ---

func TestAspAuthorized_Provider(t *testing.T) {
	aspa := ASPA{
		65001: {65100, 65200},
	}
	require.Equal(t, asp_prov, aspAuthorized(aspa, 65001, 65100))
	require.Equal(t, asp_prov, aspAuthorized(aspa, 65001, 65200))
}

func TestAspAuthorized_NotProvider(t *testing.T) {
	aspa := ASPA{
		65001: {65100, 65200},
	}
	require.Equal(t, asp_not, aspAuthorized(aspa, 65001, 65999))
}

func TestAspAuthorized_NoAttestation(t *testing.T) {
	aspa := ASPA{
		65001: {65100},
	}
	// CAS 65002 has no ASPA record
	require.Equal(t, asp_no_att, aspAuthorized(aspa, 65002, 65100))
}

func TestAspAuthorized_EmptyProviderList(t *testing.T) {
	aspa := ASPA{
		65001: {}, // has record but no providers
	}
	require.Equal(t, asp_not, aspAuthorized(aspa, 65001, 65100))
}

// --- aspVerify upstream tests ---

func TestAspVerify_Upstream_Valid(t *testing.T) {
	// path: 65001 → 65002 → 65003 (origin)
	// 65003 says 65002 is my provider, 65002 says 65001 is my provider
	aspa := ASPA{
		65003: {65002},
		65002: {65001},
	}
	path := []uint32{65001, 65002, 65003}
	result, cas, pas := aspVerify(aspa, path, false)
	require.Equal(t, aspa_valid, result)
	require.Zero(t, cas)
	require.Zero(t, pas)
}

func TestAspVerify_Upstream_Invalid(t *testing.T) {
	// 65003 says 65002 is NOT its provider (65099 is)
	aspa := ASPA{
		65003: {65099},
		65002: {65001},
	}
	path := []uint32{65001, 65002, 65003}
	result, cas, pas := aspVerify(aspa, path, false)
	require.Equal(t, aspa_invalid, result)
	require.Equal(t, uint32(65003), cas) // 65003 has ASPA but doesn't list 65002
	require.Equal(t, uint32(65002), pas)
}

func TestAspVerify_Upstream_Unknown(t *testing.T) {
	// 65002 has no ASPA record → unknown
	aspa := ASPA{
		65003: {65002},
	}
	path := []uint32{65001, 65002, 65003}
	result, cas, pas := aspVerify(aspa, path, false)
	require.Equal(t, aspa_unknown, result)
	require.Zero(t, cas)
	require.Zero(t, pas)
}

func TestAspVerify_Upstream_SingleHop(t *testing.T) {
	aspa := ASPA{}
	result, cas, pas := aspVerify(aspa, []uint32{65001}, false)
	require.Equal(t, aspa_valid, result)
	require.Zero(t, cas)
	require.Zero(t, pas)
}

func TestAspVerify_Upstream_TwoHop_Valid(t *testing.T) {
	// path: 65001 → 65002. 65002 says 65001 is provider.
	aspa := ASPA{
		65002: {65001},
	}
	result, _, _ := aspVerify(aspa, []uint32{65001, 65002}, false)
	require.Equal(t, aspa_valid, result)
}

func TestAspVerify_Upstream_TwoHop_Invalid(t *testing.T) {
	// path: 65001 → 65002. 65002 says 65099 is provider, not 65001.
	aspa := ASPA{
		65002: {65099},
	}
	result, cas, pas := aspVerify(aspa, []uint32{65001, 65002}, false)
	require.Equal(t, aspa_invalid, result)
	require.Equal(t, uint32(65002), cas)
	require.Equal(t, uint32(65001), pas)
}

// --- aspVerify downstream tests ---

func TestAspVerify_Downstream_ValleyFree(t *testing.T) {
	// path: 65001 → 65002 → 65003 (origin)
	// valley-free: origin goes up to 65002, then 65002 goes down to 65001
	// up-ramp: 65003→65002 (65003 says 65002 is provider)
	// down-ramp: 65001→65002 (65001 says 65002 is provider)
	aspa := ASPA{
		65003: {65002},
		65001: {65002},
	}
	path := []uint32{65001, 65002, 65003}
	result, cas, pas := aspVerify(aspa, path, true)
	require.Equal(t, aspa_valid, result)
	require.Zero(t, cas)
	require.Zero(t, pas)
}

func TestAspVerify_Downstream_NotValleyFree(t *testing.T) {
	// path: 65001 → 65002 → 65003 (origin)
	// all ASes have ASPA records but the path is not valley-free:
	// up-ramp: aspAuthorized(65003, 65002) → 65003 says 65099 → asp_not → break (maxUp=0, upCAS=65003, upPAS=65002)
	// down-ramp: aspAuthorized(65001, 65002) → 65001 says 65099 → asp_not → break (maxDown=0, dnCAS=65001, dnPAS=65002)
	// maxUp + maxDown = 0 < n-2 = 1 → invalid; down-ramp failure preferred (dnCAS != 0)
	aspa := ASPA{
		65003: {65099},
		65002: {65099},
		65001: {65099},
	}
	path := []uint32{65001, 65002, 65003}
	result, cas, pas := aspVerify(aspa, path, true)
	require.Equal(t, aspa_invalid, result)
	require.Equal(t, uint32(65001), cas) // down-ramp: 65001 doesn't list 65002 as provider
	require.Equal(t, uint32(65002), pas)
}

func TestAspVerify_Downstream_ShortPathCanStillBeValid(t *testing.T) {
	// path: 65001 → 65002 → 65003 (origin)
	// 65003 says 65002 is provider, while 65001 has no ASPA.
	// The draft still considers this valid: the up-ramp can extend all the way
	// to the neighbor side, and the down-ramp can degenerate to the neighbor AS.
	aspa := ASPA{
		65003: {65002},
	}
	path := []uint32{65001, 65002, 65003}
	result, _, _ := aspVerify(aspa, path, true)
	require.Equal(t, aspa_valid, result)
}

func TestAspVerify_Downstream_LongValleyFree(t *testing.T) {
	// 4-hop valley-free path: 65001 → 65002 → 65003 → 65004 (origin)
	// origin goes up: 65004→65003 (provider), 65003→65002 (provider)
	// peer goes down: 65001→65002 (provider)
	aspa := ASPA{
		65004: {65003},
		65003: {65002},
		65001: {65002},
	}
	path := []uint32{65001, 65002, 65003, 65004}
	result, _, _ := aspVerify(aspa, path, true)
	require.Equal(t, aspa_valid, result)
}

func TestAspVerify_Downstream_PeerPeering(t *testing.T) {
	// 3-hop with peering at top: 65001 → 65002 → 65003 (origin)
	// The draft allows one central hop between the up-ramp apex and down-ramp
	// apex. Here, 65002-65003 is that peer hop, so the path is still valid.
	aspa := ASPA{
		65003: {65099}, // 65003's provider is 65099, not 65002
		65002: {65099}, // 65002 has record (needed for definitive NotProvider results)
		65001: {65002},
	}
	path := []uint32{65001, 65002, 65003}
	result, _, _ := aspVerify(aspa, path, true)
	require.Equal(t, aspa_valid, result)
}

func TestAspVerify_Downstream_Tier1PeeringIsUnknown(t *testing.T) {
	// 4-hop path with a single Tier1-Tier1 peer hop in the middle:
	// 65001 → 65002 → 65003 → 65004 (origin)
	// 65002 and 65003 publish AS0 ASPAs (represented here as empty provider
	// lists), so their mutual hop is a definitive NotProvider. The outer hops
	// are unattested, so the path is deployable today but only UNKNOWN.
	aspa := ASPA{
		65002: {},
		65003: {},
	}
	path := []uint32{65001, 65002, 65003, 65004}
	result, _, _ := aspVerify(aspa, path, true)
	require.Equal(t, aspa_unknown, result)
}

func TestAspVerify_EmptyASPA(t *testing.T) {
	// no ASPA data → all hops are NoAttestation → unknown
	aspa := ASPA{}
	path := []uint32{65001, 65002, 65003}
	result1, cas1, _ := aspVerify(aspa, path, false)
	require.Equal(t, aspa_unknown, result1)
	require.Zero(t, cas1)
	result2, cas2, _ := aspVerify(aspa, path, true)
	require.Equal(t, aspa_unknown, result2)
	require.Zero(t, cas2)
}

// --- aspVerify hop tracking tests ---

func TestAspVerify_Upstream_HopAtFirstFail(t *testing.T) {
	// path: [65001, 65002, 65003]; 65002 has ASPA but doesn't list 65001 as provider
	// first check: aspAuthorized(65002, 65001) → asp_not → INVALID, CAS=65002, PAS=65001
	aspa := ASPA{
		65002: {65099},
		65003: {65002},
	}
	path := []uint32{65001, 65002, 65003}
	result, cas, pas := aspVerify(aspa, path, false)
	require.Equal(t, aspa_invalid, result)
	require.Equal(t, uint32(65002), cas)
	require.Equal(t, uint32(65001), pas)
}

func TestAspVerify_Upstream_HopAtSecondFail(t *testing.T) {
	// path: [65001, 65002, 65003]; first hop OK, second fails
	// aspAuthorized(65002, 65001) → asp_prov ✓
	// aspAuthorized(65003, 65002) → asp_not → INVALID, CAS=65003, PAS=65002
	aspa := ASPA{
		65002: {65001},
		65003: {65099},
	}
	path := []uint32{65001, 65002, 65003}
	result, cas, pas := aspVerify(aspa, path, false)
	require.Equal(t, aspa_invalid, result)
	require.Equal(t, uint32(65003), cas)
	require.Equal(t, uint32(65002), pas)
}

func TestAspVerify_Downstream_HopPrefersDnRamp(t *testing.T) {
	// path: [65001, 65002, 65003, 65004]
	// down-ramp: aspAuthorized(65001, 65002) → 65001 says 65099, not 65002 → asp_not (dnCAS=65001, dnPAS=65002)
	// up-ramp: aspAuthorized(65004, 65003) → 65004 says 65099, not 65003 → asp_not (upCAS=65004, upPAS=65003)
	// INVALID: prefer down-ramp (dnCAS != 0)
	aspa := ASPA{
		65001: {65099},
		65002: {65099},
		65003: {65099},
		65004: {65099},
	}
	path := []uint32{65001, 65002, 65003, 65004}
	result, cas, pas := aspVerify(aspa, path, true)
	require.Equal(t, aspa_invalid, result)
	require.Equal(t, uint32(65001), cas) // down-ramp failure preferred
	require.Equal(t, uint32(65002), pas)
}

// --- parseRoleName tests ---

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

// --- aspIsDownstream tests ---

func TestAspIsDownstream(t *testing.T) {
	tests := []struct {
		name       string
		role       byte
		downstream bool
	}{
		{"provider is downstream", caps.ROLE_PROVIDER, true},
		{"rs is not downstream", caps.ROLE_RS, false},
		{"rs-client is not downstream", caps.ROLE_RS_CLIENT, false},
		{"customer is not downstream", caps.ROLE_CUSTOMER, false},
		{"peer is not downstream", caps.ROLE_PEER, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.downstream, aspIsDownstream(tc.role))
		})
	}
}
