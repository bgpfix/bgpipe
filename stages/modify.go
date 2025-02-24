package stages

import (
	"fmt"
	"net/netip"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpipe/core"
)

type Modify struct {
	*core.StageBase

	opt_nexthop4 netip.Addr
	opt_nexthop6 netip.Addr
}

func NewModify(parent *core.StageBase) core.Stage {
	var (
		s = &Modify{StageBase: parent}
		o = &s.Options
	)

	o.Descr = "change or add attributes in UPDATE messages"
	o.Usage = "modify"
	o.Bidir = true

	f := o.Flags
	f.String("nexthop4", "", "use given next-hop address for IPv4 prefixes")
	f.String("nexthop6", "", "use given next-hop address for IPv6 prefixes")

	return s
}

func (s *Modify) Attach() error {
	k := s.K

	// parse NEXT_HOP4
	nh4 := k.String("nexthop4")
	if nh4 != "" {
		a, err := netip.ParseAddr(nh4)
		if err != nil || !a.Is4() {
			return fmt.Errorf("--nexthop4 %s: invalid IPv4 address", nh4)
		}
		s.opt_nexthop4 = a
	}

	// parse NEXT_HOP6
	nh6 := k.String("nexthop6")
	if nh6 != "" {
		a, err := netip.ParseAddr(nh6)
		if err != nil || !a.Is6() {
			return fmt.Errorf("--nexthop6 %s: invalid IPv6 address", nh6)
		}
		s.opt_nexthop6 = a
	}

	// register our callback
	s.P.OnMsg(s.modify, s.Dir, msg.UPDATE)
	return nil
}

func (s *Modify) modify(m *msg.Msg) (accept_message bool) {
	u := &m.Update
	mp := u.MP(attrs.ATTR_MP_REACH).Prefixes()

	// handle IPv4 prefixes
	if s.opt_nexthop4.IsValid() {
		if len(u.Reach) > 0 {
			nh := u.Attrs.Use(attrs.ATTR_NEXTHOP).(*attrs.IP)
			nh.Addr = s.opt_nexthop4
		}

		if mp != nil && mp.IsIPv4() {
			mp.NextHop = s.opt_nexthop4
		}
	}

	// handle IPv6 prefixes
	if s.opt_nexthop6.IsValid() {
		if mp != nil && mp.IsIPv6() {
			mp.NextHop = s.opt_nexthop6
		}
	}

	return true
}
