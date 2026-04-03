package rpki

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
)

// validatePrefix performs ROV for a single prefix against VRP caches.
func (s *Rpki) validatePrefix(v4, v6 VRPs, p netip.Prefix, origin uint32) int {
	var vrps VRPs
	var minLen int
	if p.Addr().Is4() {
		minLen = min_vrp_v4
		vrps = v4
	} else {
		minLen = min_vrp_v6
		vrps = v6
	}
	if len(vrps) == 0 {
		if s.strict {
			return rov_invalid
		}
		return rov_not_found
	}

	// find covering VRPs from most- to least-specific
	var found bool
	addr, bits := p.Addr(), uint8(p.Bits())
	for try := p.Bits(); try >= minLen; try-- {
		p, _ := addr.Prefix(try)
		for _, e := range vrps[p] {
			if origin != 0 && origin == e.ASN && bits <= e.MaxLen {
				return rov_valid
			}
			found = true
		}
	}

	if found {
		return rov_invalid
	} else if s.strict {
		return rov_invalid
	}
	return rov_not_found
}

// validateMsg is the callback for UPDATE messages.
func (s *Rpki) validateMsg(m *msg.Msg) bool {
	s.cnt_msg.Inc()

	u := &m.Update
	tags := pipe.UseTags(m)

	// current VRP caches
	v4, v6 := *s.vrp4.Load(), *s.vrp6.Load()

	// origin AS from AS_PATH
	origin := u.AsPath().Origin()

	// check each reachable prefix, optionally deleting invalid ones
	var valid, invalid, not_found []nlri.Prefix
	do_delete := s.rov_act == act_withdraw || s.rov_act == act_filter || s.rov_act == act_split
	check := func(p nlri.Prefix) bool {
		switch s.validatePrefix(v4, v6, p.Prefix, origin) {
		case rov_valid:
			s.cnt_rov_valid.Inc()
			valid = append(valid, p)
			if s.tag {
				tags["rpki/"+p.String()] = "VALID"
			}
			return false

		case rov_not_found:
			s.cnt_rov_nf.Inc()
			not_found = append(not_found, p)
			if s.tag {
				tags["rpki/"+p.String()] = "NOT_FOUND"
			}
			return false

		case rov_invalid:
			s.cnt_rov_inv.Inc()
			invalid = append(invalid, p)
			return do_delete
		}
		panic("unreachable")
	}

	u.Reach = slices.DeleteFunc(u.Reach, check)
	if mpp := u.ReachMP().Prefixes(); mpp != nil && mpp.Len() > 0 {
		mpp.Prefixes = slices.DeleteFunc(mpp.Prefixes, check)
	}

	// act on ROV results
	if len(invalid) > 0 {
		if s.tag || s.rov_act != act_keep {
			m.Edit()
		}

		// split invalid prefixes into separate UPDATE?
		do_split := s.rov_act == act_split && len(valid)+len(not_found) > 0
		m2, t2 := m, tags
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

		if s.tag {
			t2["rpki/status"] = "INVALID"
			for _, p := range invalid {
				t2["rpki/"+p.String()] = "INVALID"
			}
		}

		if s.rov_act == act_split || s.rov_act == act_withdraw {
			m2.Update.AddUnreach(invalid...)
		}

		// drop attributes if no reachable prefixes left
		if do_delete && len(valid)+len(not_found) == 0 {
			m2.Update.Attrs.Filter(attrs.ATTR_MP_UNREACH)
		}

		if s.event != "" {
			s.Event(s.event, m2)
		}

		if s.rov_act == act_drop {
			return false
		}

		if do_split {
			s.split.WriteMsg(m2)
			// tag the original (valid/not-found) message so downstream filters work
			if s.tag {
				switch {
				case len(valid) > 0:
					tags["rpki/status"] = "VALID"
				case len(not_found) > 0:
					tags["rpki/status"] = "NOT_FOUND"
				}
			}
		}

		if s.rov_act != act_keep && !do_split && !u.HasReach() && !u.HasUnreach() {
			return false
		}
	} else if s.tag {
		switch {
		case len(not_found) > 0:
			tags["rpki/status"] = "NOT_FOUND"
			m.Edit()
		case len(valid) > 0:
			tags["rpki/status"] = "VALID"
			m.Edit()
		}
	}

	// ASPA validation (independent of ROV, requires --aspa)
	if s.aspa_on {
		return s.validateAspa(m, u, tags)
	}
	return true
}
