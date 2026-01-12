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
func (s *Rpki) validatePrefix(roa4, roa6 *ROAS, p netip.Prefix, origin uint32) int {
	// get ROA cache
	var roas ROAS
	var minLen int
	if p.Addr().Is4() {
		minLen = minROALenV4
		if roa4 != nil {
			roas = *roa4
		}
	} else {
		minLen = minROALenV6
		if roa6 != nil {
			roas = *roa6
		}
	}
	if len(roas) == 0 {
		return RPKI_NOT_FOUND
	}

	// Check covering prefixes from most-specific to least-specific
	var found bool
	addr, bits, try := p.Addr(), uint8(p.Bits()), p.Bits()
	for {
		for _, e := range roas[p] {
			if origin == e.ASN && bits <= e.MaxLen {
				return RPKI_VALID
			} else {
				found = true
			}
		}

		// retry with less specific prefix?
		if try > minLen {
			try--
			p, _ = addr.Prefix(try)
		} else {
			break
		}
	}

	if found {
		return RPKI_INVALID
	} else if s.strict {
		return RPKI_INVALID
	} else {
		return RPKI_NOT_FOUND
	}
}

// validate is the callback for UPDATE messages
func (s *Rpki) validate(m *msg.Msg) (keep bool) {
	u := &m.Update
	mx := pipe.UseContext(m)
	keep = true

	// Get origin AS from AS_PATH
	// TODO: AS_SET paths make ROA-covered prefixes INVALID
	origin := u.AsPath().Origin()
	if origin == 0 {
		return true // empty/AS_SET origin, pass through
	}

	// get ROA caches (once)
	roa4 := s.roa4.Load()
	roa6 := s.roa6.Load()

	// check_delete checks a prefix and decides whether to delete it
	var invalid []nlri.NLRI
	check_delete := func(p nlri.NLRI) bool {
		if !keep {
			return false // already decided to drop m
		} else if s.validatePrefix(roa4, roa6, p.Prefix, origin) != RPKI_INVALID {
			return false // not bad enough, let's keep it
		}

		// drop the whole message?
		if s.invalidAction == RPKI_DROP {
			keep = false
			return false
		}

		// mark as invalid
		invalid = append(invalid, p)

		// add RPKI tags
		tags := mx.UseTags()
		tags["rpki/status"] = "INVALID"
		tags["rpki/"+p.String()] = "INVALID"

		return s.invalidAction != RPKI_TAG // delete if not just tagging
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
		if s.invalidAction == RPKI_WITHDRAW {
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
