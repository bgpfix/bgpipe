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
			valid = append(valid, p)
			if s.tag {
				tags["rpki/"+p.String()] = "VALID"
			}
			return false // keep prefix

		case rpki_not_found:
			not_found = append(not_found, p)
			if s.tag {
				tags["rpki/"+p.String()] = "NOT_FOUND"
			}
			return false // keep prefix

		case rpki_invalid:
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

	// act based on validation results
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

	// if we're here, m does not contain invalid prefixes
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

	return true
}
