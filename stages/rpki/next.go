package rpki

import (
	"net/netip"
	"slices"

	"github.com/bgpfix/bgpipe/pkg/util"
)

func (s *Rpki) nextFlush() {
	s.next4 = make(VRPs)
	s.next6 = make(VRPs)
	s.next_aspa = make(ASPA)
}

func (s *Rpki) nextApply() {
	v4, v6, aspa := s.next4, s.next6, s.next_aspa
	s.vrp4.Store(&v4)
	s.vrp6.Store(&v6)
	s.aspa.Store(&aspa)

	s.Info().Int("v4", len(v4)).Int("v6", len(v6)).Int("aspa", len(aspa)).Msg("RPKI cache updated")
	util.Close(s.vrp_done)

	// copy-on-write: clone current into next for incremental updates
	s.next4 = make(VRPs, len(v4))
	for p, entries := range v4 {
		if len(entries) > 0 {
			s.next4[p] = slices.Clone(entries)
		}
	}
	s.next6 = make(VRPs, len(v6))
	for p, entries := range v6 {
		if len(entries) > 0 {
			s.next6[p] = slices.Clone(entries)
		}
	}
	s.next_aspa = make(ASPA, len(aspa))
	for cas, provs := range aspa {
		s.next_aspa[cas] = slices.Clone(provs)
	}
}

func (s *Rpki) nextVRP(add bool, prefix netip.Prefix, maxLen uint8, asn uint32) {
	p := prefix.Masked()
	next := s.next4
	maxBits := 32
	if p.Addr().Is6() {
		next = s.next6
		maxBits = 128
	}

	if ml := int(maxLen); ml < prefix.Bits() || ml > maxBits {
		s.Warn().Str("prefix", prefix.String()).Int("maxLength", ml).Msg("invalid maxLength, skipping")
		return
	}

	entry := VRP{MaxLen: maxLen, ASN: asn}
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

func (s *Rpki) nextASPA(add bool, cas uint32, providers []uint32) {
	if add {
		s.next_aspa[cas] = slices.Clone(providers)
	} else {
		delete(s.next_aspa, cas)
	}
}
