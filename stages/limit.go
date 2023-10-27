package stages

import (
	"fmt"
	"net/netip"
	"slices"

	"github.com/bgpfix/bgpfix/af"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	bgpipe "github.com/bgpfix/bgpipe/core"
)

type Limit struct {
	*bgpipe.StageBase

	afi  af.AFI
	safi af.SAFI

	plen int // max prefix length

}

func NewLimit(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s  = &Limit{StageBase: parent}
		so = &s.Options
		sf = so.Flags
	)

	sf.IntP("length", "l", 0, "prefix length limit (0 = /24 for v4, or /48 for v6)")
	sf.IntP("count", "c", 0, "global count limit (0 = no limit)")
	sf.IntP("origin", "o", 0, "per-AS origin limit (0 = no limit)")
	sf.IntP("block", "b", 0, "per-IP block limit (0 = no limit)")
	sf.IntP("block-length", "B", 0, "IP block length (0 = /8 for v4, or /32 for v6)")

	// TODO: export to global?
	sf.StringSliceP("kill", "k", []string{"count"}, "kill the session on these events")

	so.Descr = "limit prefix lengths and counts"
	so.Usage = "limit [OPTIONS] ipv4|ipv6"
	so.Args = []string{"af"}

	so.Events = map[string]string{
		"length":           "too long prefix announced",
		"length-withdrawn": "too long prefix withdrawn",
		"count":            "too many prefixes reachable over the session",
		"origin":           "too many prefixes for a single AS origin",
		"block":            "too many prefixes for a single IP block",
	}

	so.Bidir = true // will aggregate both directions

	return s
}

func (s *Limit) Attach() error {
	k := s.K

	// address family
	switch v := k.String("af"); v {
	case "ipv4":
		s.afi = af.AFI_IPV4
		s.safi = af.SAFI_UNICAST
	case "ipv6":
		s.afi = af.AFI_IPV6
		s.safi = af.SAFI_UNICAST
	default:
		return fmt.Errorf("invalid address family: %s", v)
	}

	s.plen = k.Int("length")
	if s.plen < 0 {
		return fmt.Errorf("invalid prefix length: %d", s.plen)
	}
	if s.afi == af.AFI_IPV6 {
		if s.plen == 0 {
			s.plen = 48
		} else if s.plen > 56 {
			return fmt.Errorf("invalid IPv6 prefix length %d: max is /56", s.plen)
		}
	} else {
		if s.plen == 0 {
			s.plen = 24
		} else if s.plen > 32 {
			return fmt.Errorf("invalid IPv4 prefix length %d: max is /32", s.plen)
		}
	}

	s.P.OnMsg(s.onMsg, s.Dir, msg.UPDATE)
	return nil
}

func (s *Limit) onMsg(m *msg.Msg) (action pipe.Action) {
	s.checkUnreach(&m.Update)
	s.checkReach(&m.Update)
	return
}

func (s *Limit) checkReach(u *msg.Update) {
	dropLength := func(p netip.Prefix) bool {
		if p.Bits() <= s.plen {
			return false
		}
		s.Event("length", p.String()) //, u.Msg)
		u.Msg.Dirty = true
		return true
	}

	// any prefixes in the non-MP part?
	if s.afi == af.AFI_IPV4 {
		u.Reach = slices.DeleteFunc(u.Reach, dropLength)
	}

	// any prefixes in the MP part?
	if mp := u.ReachPrefixes(s.afi, s.safi); mp != nil {
		mp.Prefixes = slices.DeleteFunc(mp.Prefixes, dropLength)
		// TODO: what if we delete all?
	}

	// TODO: what if we delete all?

	return
}

func (s *Limit) checkUnreach(u *msg.Update) {
	dropLength := func(p netip.Prefix) bool {
		if p.Bits() <= s.plen {
			return false
		}
		s.Event("length-withdrawn", p.String()) //, u.Msg)
		u.Msg.Dirty = true
		return true
	}

	// any prefixes in the non-MP part?
	if s.afi == af.AFI_IPV4 {
		u.Unreach = slices.DeleteFunc(u.Unreach, dropLength)
	}

	// any prefixes in the MP part?
	if mp := u.UnreachPrefixes(s.afi, s.safi); mp != nil {
		mp.Prefixes = slices.DeleteFunc(mp.Prefixes, dropLength)
	}
}
