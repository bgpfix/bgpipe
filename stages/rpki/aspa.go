package rpki

import (
	"slices"
	"strings"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
)

// aspAuthorized return values
const (
	asp_no_att = 0 // CAS has no ASPA record
	asp_prov   = 1 // PAS is in CAS's provider list
	asp_not    = 2 // CAS has ASPA but PAS is not listed
)

// aspAuthorized checks ASPA authorization for a CAS→PAS hop.
func aspAuthorized(aspa ASPA, cas, pas uint32) int {
	provs, ok := aspa[cas]
	if !ok {
		return asp_no_att
	}
	for _, p := range provs {
		if p == pas {
			return asp_prov
		}
	}
	return asp_not
}

// aspVerify verifies the flat AS_PATH against ASPA.
//
// path[0] is the most-recently-traversed AS (direct peer),
// path[N-1] is the origin AS. Returns aspa_valid, aspa_unknown, or aspa_invalid.
//
// downstream=true when received from a provider or RS (downstream direction).
// downstream=false when received from a customer, peer, or RS-client (upstream).
//
// NB: does not check path[0] == neighbor AS (draft §5.4/5.5 step 2).
// The caller must do that check, skipping it for RS peers (RFC 7947).
func aspVerify(aspa ASPA, path []uint32, downstream bool) int {
	n := len(path)
	if n <= 1 {
		return aspa_valid
	}

	if !downstream {
		// upstream: every hop should go up (each AS sent to its provider)
		result := aspa_valid
		for i := 0; i < n-1; i++ {
			switch aspAuthorized(aspa, path[i+1], path[i]) {
			case asp_not:
				return aspa_invalid
			case asp_no_att:
				result = aspa_unknown
			}
		}
		return result
	}

	// downstream: find up-ramp from origin + down-ramp from peer.
	// Valid if up_ramp + down_ramp covers all N-1 pairs (valley-free).
	//
	// max counts Provider and NoAttestation until first NotProvider;
	// min counts only leading Provider hops (stops at first NoAttestation).
	maxUp, minUp := 0, 0
	exact := true
	for i := n - 2; i >= 0; i-- {
		auth := aspAuthorized(aspa, path[i+1], path[i])
		if auth == asp_not {
			break
		}
		maxUp++
		if auth == asp_prov && exact {
			minUp++
		} else {
			exact = false
		}
	}

	maxDown, minDown := 0, 0
	exact = true
	for i := 0; i < n-1; i++ {
		auth := aspAuthorized(aspa, path[i], path[i+1])
		if auth == asp_not {
			break
		}
		maxDown++
		if auth == asp_prov && exact {
			minDown++
		} else {
			exact = false
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

// aspPeerASN returns the peer's ASN from its OPEN message, or 0 if unavailable.
func aspPeerASN(p *pipe.Pipe, d dir.Dir) uint32 {
	om := p.LineFor(d).Open.Load()
	if om == nil {
		return 0
	}
	return uint32(om.GetASN())
}

// aspPeerRole reads the BGP Role capability from the peer's OPEN message.
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
// Per RFC 9234: PROVIDER → we are their customer → downstream.
// Per ASPA draft §6.3: RS is treated like a provider.
func aspIsDownstream(role byte) bool {
	return role == caps.ROLE_PROVIDER || role == caps.ROLE_RS
}

// parseRoleName converts a --aspa-role flag string to a caps.ROLE_* constant.
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

// validateAspa performs ASPA path validation for the UPDATE message.
// Returns false to drop, true to keep.
func (s *Rpki) validateAspa(m *msg.Msg, u *msg.Update, tags map[string]string) bool {
	aspa := s.aspa.Load()
	if aspa == nil || len(*aspa) == 0 {
		return true // no ASPA data
	}
	if !u.HasReach() {
		return true // withdrawal-only, no AS_PATH to validate
	}

	// NB: role resolved once per direction on first UPDATE. BGP guarantees OPEN
	// is exchanged before any UPDATE. If --aspa-role auto and peer didn't
	// send BGP Role capability, ASPA is permanently skipped for this direction.
	di := m.Dir & 1 // direction index: 0=R, 1=L
	s.peer_role_mu[di].Do(func() {
		if s.aspa_role != "auto" {
			// NB: validated in Attach()
			role, _ := parseRoleName(s.aspa_role)
			s.peer_role[di] = int(role)
			s.peer_role_ok[di] = true
			s.peer_down[di] = aspIsDownstream(role)
			s.Info().Str("role", s.aspa_role).Str("dir", m.Dir.String()).Msg("ASPA: peer role set via --aspa-role")
		} else {
			role, ok := aspPeerRole(s.P, m.Dir)
			if !ok {
				s.Warn().Str("dir", m.Dir.String()).Msg("ASPA: peer did not send BGP Role capability, skipping (use --aspa-role to override)")
				s.peer_role[di] = -1
				return
			}
			s.peer_role[di] = int(role)
			s.peer_role_ok[di] = true
			s.peer_down[di] = aspIsDownstream(role)
			s.Info().Int("role", int(role)).Bool("downstream", s.peer_down[di]).Str("dir", m.Dir.String()).Msg("ASPA: peer role detected")
		}
	})
	if !s.peer_role_ok[di] {
		return true
	}

	// empty AS_PATH = iBGP or locally-originated, nothing to validate
	if u.AsPath().Len() == 0 {
		return true
	}

	flat := u.AsPath().Unique()

	// verify path
	var result int
	if flat == nil {
		result = aspa_invalid // AS_SET present → invalid per ASPA spec §3
	} else if len(flat) > 1 {
		// NB: per draft §5.4/5.5 step 2, path[0] must equal neighbor AS.
		// RS peers don't prepend their ASN (RFC 7947).
		if s.peer_role[di] != int(caps.ROLE_RS) {
			peerASN := aspPeerASN(s.P, m.Dir)
			if peerASN == 0 {
				s.Warn().Msg("ASPA: peer ASN unknown, first-hop check skipped")
			}
			if peerASN != 0 && flat[0] != peerASN {
				result = aspa_invalid
			} else {
				result = aspVerify(*aspa, flat, s.peer_down[di])
			}
		} else {
			result = aspVerify(*aspa, flat, s.peer_down[di])
		}
	} else {
		result = aspa_valid // single-hop
	}

	// metrics
	switch result {
	case aspa_valid:
		s.cnt_aspa_valid.Inc()
	case aspa_unknown:
		s.cnt_aspa_unk.Inc()
	case aspa_invalid:
		s.cnt_aspa_inv.Inc()
	}

	// tag
	if s.aspa_tag {
		switch result {
		case aspa_valid:
			tags["aspa/status"] = "VALID"
		case aspa_unknown:
			tags["aspa/status"] = "UNKNOWN"
		case aspa_invalid:
			tags["aspa/status"] = "INVALID"
		}
		m.Edit()
	}

	if result != aspa_invalid {
		return true
	}

	// event
	if s.aspa_ev != "" {
		s.Event(s.aspa_ev, m)
	}

	// action: ASPA condemns the entire path, not individual prefixes
	switch s.aspa_act {
	case act_drop:
		return false
	case act_withdraw:
		// move all reachable prefixes to withdrawn
		reach := slices.Clone(u.Reach)
		u.Reach = nil
		if mpp := u.ReachMP().Prefixes(); mpp != nil {
			reach = append(reach, mpp.Prefixes...)
			mpp.Prefixes = nil
		}
		if len(reach) > 0 {
			u.AddUnreach(reach...)
		}
		// NB: pure withdrawal must not carry path attributes (RFC 4271 §4.3)
		if !u.HasReach() {
			u.Attrs.Filter(attrs.ATTR_MP_UNREACH)
		}
		m.Edit()
	}

	return true
}
