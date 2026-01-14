package rpki

import (
	"net/netip"
	"slices"

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

// validate is the callback for UPDATE messages
func (s *Rpki) validate(m *msg.Msg) bool {
	u := &m.Update
	tags := pipe.UseTags(m)

	// get origin AS from AS_PATH
	origin := u.AsPath().Origin()

	// check_delete checks a prefix and decides whether to delete it
	var valid, invalid, not_found []nlri.NLRI
	roa4, roa6 := *s.roa4.Load(), *s.roa6.Load()
	invalid_delete := s.invalid == rpki_withdraw || s.invalid == rpki_filter
	check_delete := func(p nlri.NLRI) bool {
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
			if s.tag {
				tags["rpki/"+p.String()] = "INVALID"
			}
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
	switch {
	case len(invalid) > 0:
		modified := false

		// any invalid prefix makes the whole message invalid
		if s.tag {
			tags["rpki/status"] = "INVALID"
			modified = true
		}

		// rewrite invalid prefixes to unreach?
		if s.invalid == rpki_withdraw {
			u.AddUnreach(invalid...)
			modified = true
		}

		// drop attributes if no reachable prefixes left?
		if invalid_delete && len(valid)+len(not_found) == 0 {
			u.Attrs.Filter(attrs.ATTR_MP_UNREACH)
			modified = true
		}

		// mark message as edited?
		m.Edit(modified)

		// send an event?
		if s.event != "" {
			s.Event(s.event, m)
		}

		// drop the message?
		if s.invalid == rpki_drop {
			return false
		}

	case len(valid) > 0:
		if s.tag {
			tags["rpki/status"] = "VALID"
			m.Edit()
		}

	case len(not_found) > 0:
		if s.tag {
			tags["rpki/status"] = "NOT_FOUND"
			m.Edit()
		}
	}

	return true
}
