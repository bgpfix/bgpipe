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
	// get ROA cache
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

	// check covering prefixes from most-specific to least-specificfou
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
func (s *Rpki) validate(m *msg.Msg) (keep bool) {
	keep = true
	u := &m.Update
	tags := pipe.UseTags(m)

	// get origin AS from AS_PATH
	origin := u.AsPath().Origin()

	// check_delete checks a prefix and decides whether to delete it
	var invalid []nlri.NLRI
	roa4, roa6 := *s.roa4.Load(), *s.roa6.Load()
	check_delete := func(p nlri.NLRI) bool {
		if !keep {
			return false // already decided to drop m
		} else if s.validatePrefix(roa4, roa6, p.Prefix, origin) != rpki_invalid {
			return false // not bad enough, let's keep it
		}

		// drop the whole message?
		if s.invalid == rpki_drop {
			keep = false
			return false
		}

		// mark as invalid
		invalid = append(invalid, p)

		// add RPKI tags
		tags["rpki/status"] = "INVALID"
		tags["rpki/"+p.String()] = "INVALID"

		return s.invalid != rpki_tag // delete if not just tagging
	}

	// check IPv4 reachable prefixes
	u.Reach = slices.DeleteFunc(u.Reach, check_delete)
	if !keep {
		return false
	}

	// check MP reachable prefixes
	mpp := u.ReachMP().Prefixes()
	if mpp != nil && mpp.Len() > 0 {
		mpp.Prefixes = slices.DeleteFunc(mpp.Prefixes, check_delete)
		if !keep {
			return false
		}
	}

	// check the result
	if len(invalid) > 0 {
		// need to write invalid prefixes to unreach?
		if s.invalid == rpki_withdraw {
			u.AddUnreach(invalid...)
		}

		// clean up MP unreach if now empty
		if mpp != nil && mpp.Len() == 0 {
			u.Attrs.Drop(attrs.ATTR_MP_REACH)
		}

		// mark message as edited
		m.Edit()
	}

	return true
}
