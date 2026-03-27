package rpki

import (
	"net/netip"
	"slices"

	"github.com/bgpfix/bgpipe/pkg/util"
)

func (s *Rpki) nextFlush() {
	s.next4 = make(ROA)
	s.next6 = make(ROA)
	s.nextAspa = make(ASPA)
}

func (s *Rpki) nextApply() {
	// atomically publish the pending caches as current
	roa4, roa6, aspa := s.next4, s.next6, s.nextAspa
	s.roa4.Store(&roa4)
	s.roa6.Store(&roa6)
	s.aspa.Store(&aspa)

	// signal that the cache is ready (once)
	s.Info().Int("v4", len(roa4)).Int("v6", len(roa6)).Int("aspa", len(aspa)).Msg("RPKI cache updated")
	util.Close(s.roa_done)

	// make copy-on-write next caches from current
	s.next4 = make(ROA, len(roa4))
	for p, entries := range roa4 {
		if len(entries) > 0 {
			s.next4[p] = slices.Clone(entries)
		}
	}
	s.next6 = make(ROA, len(roa6))
	for p, entries := range roa6 {
		if len(entries) > 0 {
			s.next6[p] = slices.Clone(entries)
		}
	}
	s.nextAspa = make(ASPA, len(aspa))
	for cas, provs := range aspa {
		s.nextAspa[cas] = slices.Clone(provs)
	}
}

func (s *Rpki) nextRoa(add bool, prefix netip.Prefix, maxLen uint8, asn uint32) {
	p := prefix.Masked()
	next := s.next4
	maxBits := 32
	if p.Addr().Is6() {
		next = s.next6
		maxBits = 128
	}

	if ml := int(maxLen); ml < prefix.Bits() || ml > maxBits {
		s.Warn().Str("prefix", prefix.String()).Int("maxLength", ml).Msg("invalid MaxLength, skipping")
		return
	}

	entry := ROAEntry{MaxLen: maxLen, ASN: asn}
	i := slices.Index(next[p], entry)

	if add {
		if i < 0 {
			next[p] = append(next[p], entry)
		}
	} else {
		if i >= 0 {
			next[p] = slices.Delete(next[p], i, i+1)
		}
	}
}

// nextAspaEntry adds or removes a single ASPA record in the pending cache.
func (s *Rpki) nextAspaEntry(add bool, cas uint32, providers []uint32) {
	if add {
		s.nextAspa[cas] = slices.Clone(providers)
	} else {
		delete(s.nextAspa, cas)
	}
}
