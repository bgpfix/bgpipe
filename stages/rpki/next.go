package rpki

import (
	"net/netip"
	"slices"

	"github.com/bgpfix/bgpipe/pkg/util"
)

func (s *Rpki) nextFlush() {
	s.next4 = make(ROA)
	s.next6 = make(ROA)
}

func (s *Rpki) nextApply() {
	// publish next as current
	roa4, roa6 := s.next4, s.next6
	s.roa4.Store(&roa4)
	s.roa6.Store(&roa6)

	// signal the ROA is ready
	s.Info().Int("v4", len(roa4)).Int("v6", len(roa6)).Msg("ROA cache updated")
	util.Close(s.roaReady)

	// make next copies of current maps
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
}

func (s *Rpki) nextRoa(add bool, prefix netip.Prefix, maxLen uint8, asn uint32) {
	next := s.next4

	// check maxLen
	if ml := int(maxLen); ml < prefix.Bits() || ml > 128 {
		s.Warn().Str("prefix", prefix.String()).Int("maxLength", ml).Msg("invalid MaxLength, skipping")
		return
	}

	// is IPv6?
	p := prefix.Masked()
	if p.Addr().Is6() {
		next = s.next6
	}

	// entry already exists?
	entry := ROAEntry{MaxLen: maxLen, ASN: asn}
	i := slices.Index(next[p], entry)

	if add { // add iff really novel
		if i < 0 {
			next[p] = append(next[p], entry)
		}
	} else { // drop if really exists
		if i >= 0 {
			next[p] = slices.Delete(next[p], i, i+1)
		}
	}
}
