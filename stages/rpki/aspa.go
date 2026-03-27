package rpki

import (
	"strings"

	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/pipe"
)

// aspAuthorized return values
const (
	aspNoAttestation = 0 // CAS has no ASPA record
	aspProvider      = 1 // PAS is in CAS's provider list
	aspNotProvider   = 2 // CAS has ASPA but PAS is not listed
)

// aspAuthorized checks ASPA authorization for a CAS→PAS hop.
func aspAuthorized(aspa ASPA, cas, pas uint32) int {
	provs, ok := aspa[cas]
	if !ok {
		return aspNoAttestation
	}
	for _, p := range provs {
		if p == pas {
			return aspProvider
		}
	}
	return aspNotProvider
}

// aspVerify verifies the flat AS_PATH against ASPA.
//
// path[0] is the most-recently-traversed AS (our direct peer),
// path[N-1] is the origin AS. Returns aspa_valid, aspa_unknown, or aspa_invalid.
//
// downstream=true when the route was received from a provider or RS (downstream direction).
// downstream=false when received from a customer, peer, or RS-client (upstream direction).
//
// NB: does not check that path[0] equals the neighbor AS (draft §5.4 step 2 / §5.5 step 2).
// That check must be done by the caller using the peer's ASN from the OPEN message.
// It is skipped when the peer is an RS (RS doesn't prepend its own ASN per RFC 7947).
func aspVerify(aspa ASPA, path []uint32, downstream bool) int {
	n := len(path)
	if n <= 1 {
		return aspa_valid
	}

	if !downstream {
		// upstream path: every hop should go up (each AS sent to its provider).
		// For pair (path[i+1], path[i]): check if path[i] is a provider of path[i+1].
		result := aspa_valid
		for i := 0; i < n-1; i++ {
			switch aspAuthorized(aspa, path[i+1], path[i]) {
			case aspNotProvider:
				return aspa_invalid
			case aspNoAttestation:
				result = aspa_unknown
			}
		}
		return result
	}

	// downstream path: find up-ramp from origin and down-ramp from peer.
	// Valid if up_ramp + down_ramp covers all N-1 pairs (valley-free path).
	//
	// up-ramp: from origin (path[N-1]), each AS sent to its provider (path[i] is provider of path[i+1]).
	// Scan right-to-left: check aspAuthorized(path[i+1], path[i]) for i = N-2 downto 0.
	//
	// down-ramp: from peer (path[0]), each AS received from its provider (path[i+1] is provider of path[i]).
	// Scan left-to-right: check aspAuthorized(path[i], path[i+1]) for i = 0 to N-2.
	//
	// NB: max counts Provider+ and No Attestation (ambiguous, possibly part of ramp) until first
	// Not-Provider+; min counts only leading Provider+ hops (stops at first No Attestation).
	// Distinction matters when some ASes in the path lack ASPA records.

	maxUp, minUp := 0, 0
	minUpExact := true // false after first No Attestation hop
	for i := n - 2; i >= 0; i-- {
		auth := aspAuthorized(aspa, path[i+1], path[i])
		if auth == aspNotProvider {
			break
		}
		maxUp++
		if auth == aspProvider && minUpExact {
			minUp++
		} else {
			minUpExact = false
		}
	}

	maxDown, minDown := 0, 0
	minDownExact := true
	for i := 0; i < n-1; i++ {
		auth := aspAuthorized(aspa, path[i], path[i+1])
		if auth == aspNotProvider {
			break
		}
		maxDown++
		if auth == aspProvider && minDownExact {
			minDown++
		} else {
			minDownExact = false
		}
	}

	if maxUp+maxDown < n-1 {
		return aspa_invalid
	}
	if minUp+minDown < n-1 {
		return aspa_unknown
	}
	return aspa_valid
}

// aspPeerRole reads the BGP Role capability from the peer's OPEN message.
// Returns the role byte and true if present; false if the peer didn't send the capability.
func aspPeerRole(p *pipe.Pipe, d dir.Dir) (byte, bool) {
	om := p.LineFor(d).Open.Load()
	if om == nil {
		return 0, false
	}
	c, ok := om.Caps.Get(caps.CAP_ROLE).(*caps.Role)
	if !ok || c == nil {
		return 0, false
	}
	return c.Role, true
}

// aspIsDownstream maps the peer's BGP Role to the downstream flag.
// Returns true if the route came from a provider or Route Server (downstream direction).
// Per RFC 9234: PROVIDER → we are their customer → downstream.
// Per ASPA draft §6.3: RS is treated like a provider for ASPA verification purposes.
func aspIsDownstream(peerRole byte) bool {
	return peerRole == caps.ROLE_PROVIDER || peerRole == caps.ROLE_RS
}

// parseRoleName converts a --role flag string to a caps.ROLE_* constant.
// Returns (role, true) on success, (0, false) on unknown name.
func parseRoleName(name string) (byte, bool) {
	switch strings.ToLower(name) {
	case "provider":
		return caps.ROLE_PROVIDER, true
	case "rs":
		return caps.ROLE_RS, true
	case "rs-client":
		return caps.ROLE_RS_CLIENT, true
	case "customer":
		return caps.ROLE_CUSTOMER, true
	case "peer":
		return caps.ROLE_PEER, true
	default:
		return 0, false
	}
}
