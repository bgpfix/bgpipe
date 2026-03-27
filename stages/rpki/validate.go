package rpki

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
)

// validatePrefix performs RPKI validation for a single prefix
func (s *Rpki) validatePrefix(roa4, roa6 ROA, p netip.Prefix, origin uint32) int {
	// pick ROA cache
	var roas ROA
	var minLen int
	if p.Addr().Is4() {
		minLen = minROALenV4
		roas = roa4
	} else {
		minLen = minROALenV6
		roas = roa6
	}
	if len(roas) == 0 {
		if s.strict {
			return rpki_invalid
		}
		return rpki_not_found
	}

	// find covering prefixes from most- to least-specific
	var found bool
	addr, bits := p.Addr(), uint8(p.Bits())
	for try := p.Bits(); try >= minLen; try-- {
		p, _ := addr.Prefix(try)
		for _, e := range roas[p] {
			if origin != 0 && origin == e.ASN && bits <= e.MaxLen {
				return rpki_valid
			} else {
				found = true
			}
		}
	}

	if found {
		return rpki_invalid
	} else if s.strict {
		return rpki_invalid
	} else {
		return rpki_not_found
	}
}

// validateMsg is the callback for UPDATE messages
func (s *Rpki) validateMsg(m *msg.Msg) bool {
	s.cMessages.Inc()

	u := &m.Update
	tags := pipe.UseTags(m)

	// get current ROA caches
	roa4, roa6 := *s.roa4.Load(), *s.roa6.Load()

	// get origin AS from AS_PATH
	origin := u.AsPath().Origin()

	// check_delete checks a prefix and decides whether to delete it
	var valid, invalid, not_found []nlri.Prefix
	invalid_delete := s.invalid == rpki_withdraw || s.invalid == rpki_filter || s.invalid == rpki_split
	check_delete := func(p nlri.Prefix) bool {
		switch s.validatePrefix(roa4, roa6, p.Prefix, origin) {
		case rpki_valid:
			s.cValid.Inc()
			valid = append(valid, p)
			if s.tag {
				tags["rpki/"+p.String()] = "VALID"
			}
			return false // keep prefix

		case rpki_not_found:
			s.cNotFound.Inc()
			not_found = append(not_found, p)
			if s.tag {
				tags["rpki/"+p.String()] = "NOT_FOUND"
			}
			return false // keep prefix

		case rpki_invalid:
			s.cInvalid.Inc()
			invalid = append(invalid, p)
			return invalid_delete // drop prefix iff requested
		}
		panic("unreachable")
	}

	// check IPv4 reachable prefixes
	u.Reach = slices.DeleteFunc(u.Reach, check_delete)

	// check MP reachable prefixes
	mpp := u.ReachMP().Prefixes()
	if mpp != nil && mpp.Len() > 0 {
		mpp.Prefixes = slices.DeleteFunc(mpp.Prefixes, check_delete)
	}

	// act based on ROV validation results
	if len(invalid) > 0 {
		// message (will be) modified?
		if s.tag || s.invalid != rpki_keep {
			m.Edit()
		}

		// split into separate message?
		do_split := s.invalid == rpki_split && len(valid)+len(not_found) > 0
		m2 := m // otherwise just edit the original
		t2 := tags
		if do_split {
			m2 = s.P.GetMsg().Switch(msg.UPDATE)
			m2.Time = m.Time

			t2 = pipe.UseTags(m2)
			for k, v := range tags {
				if !strings.HasPrefix(k, "rpki/") {
					t2[k] = v
				}
			}
		}

		// add RPKI tags?
		if s.tag {
			t2["rpki/status"] = "INVALID"
			for _, p := range invalid {
				t2["rpki/"+p.String()] = "INVALID"
			}
		}

		// rewrite invalid prefixes to unreach?
		if s.invalid == rpki_split || s.invalid == rpki_withdraw {
			m2.Update.AddUnreach(invalid...)
		}

		// drop attributes if no reachable prefixes left?
		if invalid_delete && len(valid)+len(not_found) == 0 {
			m2.Update.Attrs.Filter(attrs.ATTR_MP_UNREACH)
		}

		// send an event?
		if s.event != "" {
			s.Event(s.event, m2)
		}

		// drop the message?
		if s.invalid == rpki_drop {
			return false
		} else if s.invalid == rpki_keep {
			return true
		} else if do_split {
			s.in_split.WriteMsg(m2)
			// NB: original message will continue below
		} else {
			return u.HasReach() || u.HasUnreach()
		}
	}

	// if we're here, m does not contain ROV-invalid prefixes
	if s.tag {
		switch {
		case len(not_found) > 0:
			tags["rpki/status"] = "NOT_FOUND"
			m.Edit()

		case len(valid) > 0:
			tags["rpki/status"] = "VALID"
			m.Edit()
		}
	}

	// ASPA validation (independent of ROV result)
	keep, err := s.validateAspa(m, u, tags)
	if err != nil {
		s.Fatal().Err(err).Msg("ASPA role error")
		return false
	}
	return keep
}

// validateAspa performs ASPA path validation for the UPDATE message.
// Returns (false, nil) to drop the message, (true, nil) to keep it, or (false, err) on fatal error.
func (s *Rpki) validateAspa(m *msg.Msg, u *msg.Update, tags map[string]string) (bool, error) {
	aspa := s.aspa.Load()
	if aspa == nil || len(*aspa) == 0 {
		return true, nil // no ASPA data, skip
	}
	if !u.HasReach() {
		return true, nil // withdrawal-only UPDATE has no AS_PATH to validate
	}

	// NB: role is resolved exactly once, on the first UPDATE. BGP guarantees OPEN+KEEPALIVE
	// are exchanged before any UPDATE, so p.R/L.Open should be populated by the time we
	// get here. If --role auto and the peer didn't send the BGP Role capability, ASPA is
	// permanently skipped for this session; use --role to override.
	var resolveErr error
	s.peer_role_mu.Do(func() {
		roleName := s.role_name
		if roleName != "auto" {
			role, ok := parseRoleName(roleName)
			if !ok {
				resolveErr = fmt.Errorf("unknown --role value: %q", roleName)
				return
			}
			s.peer_role = int(role)
			s.peer_role_ok = true
			s.peer_downstream = aspIsDownstream(role)
			s.Info().Str("role", roleName).Msg("ASPA: peer role set via --role flag")
		} else {
			peerRole, ok := aspPeerRole(s.P, m.Dir)
			if !ok {
				// no BGP Role capability → skip ASPA (user can use --role to force)
				s.Warn().Msg("ASPA: peer did not send BGP Role capability, skipping ASPA validation (use --role to override)")
				s.peer_role = -1
				s.peer_role_ok = false
				return
			}
			s.peer_role = int(peerRole)
			s.peer_role_ok = true
			s.peer_downstream = aspIsDownstream(peerRole)
			s.Info().Int("role", int(peerRole)).Bool("downstream", s.peer_downstream).Msg("ASPA: peer role detected via BGP Role capability")
		}
	})
	if resolveErr != nil {
		return false, resolveErr
	}
	if !s.peer_role_ok {
		return true, nil // role not available, skip ASPA
	}

	flat := u.AsPath().Flat()

	// verify
	var result int
	if flat == nil {
		result = aspa_invalid // AS_SET present or empty path → invalid per spec
	} else {
		result = aspVerify(*aspa, flat, s.peer_downstream)
	}

	// update metrics
	switch result {
	case aspa_valid:
		s.cAspaValid.Inc()
	case aspa_unknown:
		s.cAspaUnknown.Inc()
	case aspa_invalid:
		s.cAspaInvalid.Inc()
	}

	// tag the message?
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

	// nothing more to do unless INVALID
	if result != aspa_invalid {
		return true, nil
	}

	// send an event?
	if s.aspa_event != "" {
		s.Event(s.aspa_event, m)
	}

	// apply action
	switch s.aspa_action {
	case rpki_keep:
		// nothing
	case rpki_drop:
		return false, nil
	case rpki_withdraw, rpki_filter:
		invalid_prefixes := drainReachable(u)
		if len(invalid_prefixes) > 0 {
			u.AddUnreach(invalid_prefixes...)
		}
		m.Edit()
	case rpki_split:
		invalid_prefixes := drainReachable(u)
		m.Edit()
		if len(invalid_prefixes) > 0 && s.in_split != nil {
			m2 := s.P.GetMsg().Switch(msg.UPDATE)
			m2.Time = m.Time
			m2.Update.AddUnreach(invalid_prefixes...)
			s.in_split.WriteMsg(m2)
		}
	}

	return true, nil
}

// drainReachable collects all reachable prefixes (IPv4 and MP) into a slice,
// clearing them from the UPDATE in the process.
func drainReachable(u *msg.Update) []nlri.Prefix {
	prefixes := slices.Clone(u.Reach)
	u.Reach = nil
	if mpp := u.ReachMP().Prefixes(); mpp != nil {
		prefixes = append(prefixes, mpp.Prefixes...)
		mpp.Prefixes = nil
	}
	return prefixes
}
