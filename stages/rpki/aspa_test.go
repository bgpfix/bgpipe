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
	require.Equal(t, aspa_valid, aspVerify(aspa, path, false))
}

func TestAspVerify_Upstream_Invalid(t *testing.T) {
	// 65003 says 65002 is NOT its provider (65099 is)
	aspa := ASPA{
		65003: {65099},
		65002: {65001},
	}
	path := []uint32{65001, 65002, 65003}
	require.Equal(t, aspa_invalid, aspVerify(aspa, path, false))
}

func TestAspVerify_Upstream_Unknown(t *testing.T) {
	// 65002 has no ASPA record → unknown
	aspa := ASPA{
		65003: {65002},
	}
	path := []uint32{65001, 65002, 65003}
	require.Equal(t, aspa_unknown, aspVerify(aspa, path, false))
}

func TestAspVerify_Upstream_SingleHop(t *testing.T) {
	aspa := ASPA{}
	require.Equal(t, aspa_valid, aspVerify(aspa, []uint32{65001}, false))
}

func TestAspVerify_Upstream_TwoHop_Valid(t *testing.T) {
	// path: 65001 → 65002. 65002 says 65001 is provider.
	aspa := ASPA{
		65002: {65001},
	}
	require.Equal(t, aspa_valid, aspVerify(aspa, []uint32{65001, 65002}, false))
}

func TestAspVerify_Upstream_TwoHop_Invalid(t *testing.T) {
	// path: 65001 → 65002. 65002 says 65099 is provider, not 65001.
	aspa := ASPA{
		65002: {65099},
	}
	require.Equal(t, aspa_invalid, aspVerify(aspa, []uint32{65001, 65002}, false))
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
	require.Equal(t, aspa_valid, aspVerify(aspa, path, true))
}

func TestAspVerify_Downstream_NotValleyFree(t *testing.T) {
	// path: 65001 → 65002 → 65003 (origin)
	// all ASes have ASPA records but the path is not valley-free:
	// up-ramp: aspAuthorized(65003, 65002) → 65003 says 65099, not 65002 → NotProvider → break (maxUp=0)
	// down-ramp: aspAuthorized(65001, 65002) → 65001 says 65099, not 65002 → NotProvider → break (maxDown=0)
	// maxUp + maxDown = 0 < 2 → invalid
	aspa := ASPA{
		65003: {65099},
		65002: {65099},
		65001: {65099},
	}
	path := []uint32{65001, 65002, 65003}
	require.Equal(t, aspa_invalid, aspVerify(aspa, path, true))
}

func TestAspVerify_Downstream_Unknown(t *testing.T) {
	// path: 65001 → 65002 → 65003 (origin)
	// 65003 says 65002 is provider (up-ramp=1)
	// 65001 has no ASPA (down max=1, min=0)
	// maxUp + maxDown = 1 + 1 = 2 >= 2 ✓
	// minUp + minDown = 1 + 0 = 1 < 2 → unknown
	aspa := ASPA{
		65003: {65002},
	}
	path := []uint32{65001, 65002, 65003}
	require.Equal(t, aspa_unknown, aspVerify(aspa, path, true))
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
	require.Equal(t, aspa_valid, aspVerify(aspa, path, true))
}

func TestAspVerify_Downstream_PeerPeering(t *testing.T) {
	// 3-hop with peering at top: 65001 → 65002 → 65003 (origin)
	// all ASes have ASPA records so all lookups are definitive:
	// up-ramp: aspAuthorized(65003, 65002) → 65003 says 65099, not 65002 → NotProvider → maxUp=0
	// down-ramp: aspAuthorized(65001, 65002) → 65001 says 65002 → Provider → maxDown=1
	// maxUp+maxDown=0+1=1 < 2 → invalid
	aspa := ASPA{
		65003: {65099}, // 65003's provider is 65099, not 65002
		65002: {65099}, // 65002 has record (needed for definitive NotProvider results)
		65001: {65002},
	}
	path := []uint32{65001, 65002, 65003}
	require.Equal(t, aspa_invalid, aspVerify(aspa, path, true))
}

func TestAspVerify_EmptyASPA(t *testing.T) {
	// no ASPA data → all hops are NoAttestation → unknown
	aspa := ASPA{}
	path := []uint32{65001, 65002, 65003}
	require.Equal(t, aspa_unknown, aspVerify(aspa, path, false))
	require.Equal(t, aspa_unknown, aspVerify(aspa, path, true))
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
