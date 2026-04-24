package rpki

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/stretchr/testify/require"
)

// NB: end-to-end coverage for: file cache load → pipe callback wiring →
// UPDATE validation → ROV/ASPA actions. Unit tests in validate_*_test.go
// call validateMsg directly and do not exercise file loading, cache swap,
// or actual pipe flow.

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

// newIntegrationRpki writes the fixture to a temp file and loads it
// synchronously via fileLoad, yielding a ready-to-start stage.
func newIntegrationRpki(t *testing.T) *Rpki {
	t.Helper()
	file := filepath.Join(t.TempDir(), "rpki.json")
	require.NoError(t, os.WriteFile(file, []byte(integrationFixture), 0o644))

	s := newValidateTestRpki()
	s.P.Options.Logger = nil
	s.P.Options.Caps = false
	s.file = file
	s.Dir = dir.DIR_R
	require.NoError(t, s.fileLoad())
	return s
}

// startPipe wires validateMsg into the pipe and starts it.
// Pipe is stopped automatically at test teardown.
func startPipe(t *testing.T, s *Rpki) {
	t.Helper()
	s.P.Options.OnMsg(s.validateMsg, s.Dir, msg.UPDATE)
	require.NoError(t, s.P.Start())
	t.Cleanup(func() { s.P.Stop() })
}

// sendUpdate writes m to R and returns the processed message from R.Out,
// or nil if dropped by a callback within timeout.
func sendUpdate(t *testing.T, s *Rpki, m *msg.Msg, timeout time.Duration) *msg.Msg {
	t.Helper()
	require.NoError(t, s.P.R.WriteMsg(m))
	select {
	case out := <-s.P.R.Out:
		return out
	case <-time.After(timeout):
		return nil
	}
}

func TestIntegration_CacheLoadedFromFile(t *testing.T) {
	s := newIntegrationRpki(t)
	require.Len(t, *s.vrp4.Load(), 3)
	require.Len(t, *s.aspa.Load(), 3)
	// NB: nextASPA sorts providers for BinarySearch
	require.Equal(t, []uint32{65010}, (*s.aspa.Load())[65020])
}

func TestIntegration_ROV_WithdrawMovesInvalidToUnreach(t *testing.T) {
	s := newIntegrationRpki(t)
	s.rov_act = act_withdraw
	s.tag = true
	startPipe(t, s)

	// 192.0.2.0/24 VALID (origin 65001 matches VRP), 198.51.100.0/24
	// INVALID (origin 65001 but VRP wants 65002)
	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24", "198.51.100.0/24")
	setAsPathSeq(m, 65001)

	got := sendUpdate(t, s, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, []string{"192.0.2.0/24"}, prefixStrings(got.Update.AllReach()))
	require.Equal(t, []string{"198.51.100.0/24"}, prefixStrings(got.Update.AllUnreach()))
	require.Equal(t, "INVALID", pipe.UseTags(got)["rpki/status"])
}

func TestIntegration_ROV_DropDiscardsMessage(t *testing.T) {
	s := newIntegrationRpki(t)
	s.rov_act = act_drop
	s.tag = true
	startPipe(t, s)

	m := newReachUpdate(dir.DIR_R, "198.51.100.0/24")
	setAsPathSeq(m, 65099) // no VRP with origin 65099

	require.Nil(t, sendUpdate(t, s, m, 200*time.Millisecond))
}

func TestIntegration_ROV_NotFoundPassesThrough(t *testing.T) {
	s := newIntegrationRpki(t)
	s.rov_act = act_withdraw
	s.tag = true
	startPipe(t, s)

	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65001)

	got := sendUpdate(t, s, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "NOT_FOUND", pipe.UseTags(got)["rpki/status"])
	require.Equal(t, []string{"10.0.0.0/8"}, prefixStrings(got.Update.AllReach()))
}

func TestIntegration_ASPA_ValidPathPassesThrough(t *testing.T) {
	s := newIntegrationRpki(t)
	s.aspa_on = true
	s.aspa_role = "customer"
	s.aspa_act = act_withdraw
	s.aspa_tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s)

	// path [65010, 65020]: peer matches path[0]; 65020→{65010} is attested
	m := newReachUpdate(dir.DIR_R, "203.0.113.0/24")
	setAsPathSeq(m, 65010, 65020)

	got := sendUpdate(t, s, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "VALID", pipe.UseTags(got)["aspa/status"])
	require.Equal(t, []string{"203.0.113.0/24"}, prefixStrings(got.Update.AllReach()))
}

func TestIntegration_ASPA_InvalidPath_WithdrawStripsReach(t *testing.T) {
	s := newIntegrationRpki(t)
	s.rov_act = act_keep // NB: isolate ASPA from ROV
	s.aspa_on = true
	s.aspa_role = "customer"
	s.aspa_act = act_withdraw
	s.aspa_tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s)

	// 10.0.0.0/8 has no VRP → ROV NOT_FOUND → reach preserved for ASPA.
	// path [65010, 65099]: 65099 has ASPA={65030}, 65010 not listed → INVALID
	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65010, 65099)

	got := sendUpdate(t, s, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "INVALID", pipe.UseTags(got)["aspa/status"])
	require.False(t, got.Update.HasReach())
	require.Equal(t, []string{"10.0.0.0/8"}, prefixStrings(got.Update.AllUnreach()))
	// NB: RFC 4271 §4.3 - pure withdrawal must not carry path attributes
	require.False(t, got.Update.Attrs.Has(attrs.ATTR_MP_REACH))
}

func TestIntegration_ASPA_InvalidPath_DropDiscardsMessage(t *testing.T) {
	s := newIntegrationRpki(t)
	s.rov_act = act_keep
	s.aspa_on = true
	s.aspa_role = "customer"
	s.aspa_act = act_drop
	s.aspa_tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s)

	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65010, 65099)

	require.Nil(t, sendUpdate(t, s, m, 200*time.Millisecond))
}

func TestIntegration_ASPA_InvalidPath_KeepTagsOnly(t *testing.T) {
	s := newIntegrationRpki(t)
	s.rov_act = act_keep
	s.aspa_on = true
	s.aspa_role = "customer"
	s.aspa_act = act_keep
	s.aspa_tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s)

	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65010, 65099)

	got := sendUpdate(t, s, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "INVALID", pipe.UseTags(got)["aspa/status"])
	require.Equal(t, []string{"10.0.0.0/8"}, prefixStrings(got.Update.AllReach()))
	require.False(t, got.Update.HasUnreach())
}

func TestIntegration_ASPA_UnknownPathPassesThrough(t *testing.T) {
	s := newIntegrationRpki(t)
	s.rov_act = act_keep
	s.aspa_on = true
	s.aspa_role = "customer"
	s.aspa_act = act_withdraw
	s.aspa_tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s)

	// 65040 has no ASPA record → every hop is "no_att" → UNKNOWN
	m := newReachUpdate(dir.DIR_R, "10.0.0.0/8")
	setAsPathSeq(m, 65010, 65040)

	got := sendUpdate(t, s, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "UNKNOWN", pipe.UseTags(got)["aspa/status"])
	require.Equal(t, []string{"10.0.0.0/8"}, prefixStrings(got.Update.AllReach()))
}

func TestIntegration_ASPA_FirstHopMismatchIsInvalid(t *testing.T) {
	s := newIntegrationRpki(t)
	s.aspa_on = true
	s.aspa_role = "customer"
	s.aspa_act = act_withdraw
	s.aspa_tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s)

	// path[0]=65099 != peer 65010 → INVALID regardless of ASPA content
	m := newReachUpdate(dir.DIR_R, "203.0.113.0/24")
	setAsPathSeq(m, 65099, 65020)

	got := sendUpdate(t, s, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "INVALID", pipe.UseTags(got)["aspa/status"])
	require.False(t, got.Update.HasReach())
}

func TestIntegration_ROVValid_ASPAInvalid_BothActionsFire(t *testing.T) {
	s := newIntegrationRpki(t)
	s.rov_act = act_withdraw
	s.tag = true
	s.aspa_on = true
	s.aspa_role = "customer"
	s.aspa_act = act_withdraw
	s.aspa_tag = true
	storeOpenASN(s.P, dir.DIR_R, 65010)
	startPipe(t, s)

	// 192.0.2.0/24 origin 65001 → ROV VALID; path breaks ASPA at 65099
	m := newReachUpdate(dir.DIR_R, "192.0.2.0/24")
	setAsPathSeq(m, 65010, 65099, 65001)

	got := sendUpdate(t, s, m, time.Second)
	require.NotNil(t, got)
	require.Equal(t, "VALID", pipe.UseTags(got)["rpki/status"])
	require.Equal(t, "INVALID", pipe.UseTags(got)["aspa/status"])
	require.False(t, got.Update.HasReach())
	require.Equal(t, []string{"192.0.2.0/24"}, prefixStrings(got.Update.AllUnreach()))
}

func TestIntegration_FileReloadReplacesCache(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rpki.json")
	require.NoError(t, os.WriteFile(file,
		[]byte(`{"roas":[{"prefix":"192.0.2.0/24","maxLength":24,"asn":65001}],"aspas":[]}`),
		0o644))

	s := newValidateTestRpki()
	s.P.Options.Logger = nil
	s.file = file
	require.NoError(t, s.fileLoad())
	require.Len(t, *s.vrp4.Load(), 1)
	require.Empty(t, *s.aspa.Load())

	newData := `{
		"roas": [
			{"prefix":"192.0.2.0/24","maxLength":24,"asn":65001},
			{"prefix":"10.0.0.0/8","maxLength":24,"asn":64512}
		],
		"aspas": [{"customer_asid":65010,"provider_asids":[65001]}]
	}`
	require.NoError(t, os.WriteFile(file, []byte(newData), 0o644))
	// NB: fileLoad short-circuits unless mtime advanced
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(file, future, future))

	require.NoError(t, s.fileLoad())
	require.Len(t, *s.vrp4.Load(), 2)
	require.Len(t, *s.aspa.Load(), 1)
}
