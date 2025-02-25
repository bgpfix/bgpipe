package stages

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpipe/core"
)

type Attr struct {
	*core.StageBase

	opt_nexthop4 netip.Addr
	opt_nexthop6 netip.Addr
	opt_add_com  attrs.Community
}

func NewAttr(parent *core.StageBase) core.Stage {
	var (
		s = &Attr{StageBase: parent}
		o = &s.Options
	)

	o.Descr = "change or add attributes in UPDATE messages"
	o.Bidir = true

	f := o.Flags
	f.String("set-nexthop4", "", "use given next-hop address for IPv4 prefixes")
	f.String("set-nexthop6", "", "use given next-hop address for IPv6 prefixes")
	f.StringSlice("add-com", nil, "add given BGP community value(s)")

	return s
}

func (s *Attr) Attach() error {
	k := s.K

	// parse --set-nexthop4
	nh4 := k.String("set-nexthop4")
	if nh4 != "" {
		a, err := netip.ParseAddr(nh4)
		if err != nil || !a.Is4() {
			return fmt.Errorf("--nexthop4 %s: invalid IPv4 address", nh4)
		}
		s.opt_nexthop4 = a
	}

	// parse --set-nexthop6
	nh6 := k.String("set-nexthop6")
	if nh6 != "" {
		a, err := netip.ParseAddr(nh6)
		if err != nil || !a.Is6() {
			return fmt.Errorf("--nexthop6 %s: invalid IPv6 address", nh6)
		}
		s.opt_nexthop6 = a
	}

	// parse --add-com
	for _, com := range k.Strings("add-com") {
		com1, com2, ok := strings.Cut(com, ":")
		if !ok {
			return fmt.Errorf("--add-com %s: invalid format, need ASN:VALUE", com)
		}
		asn, err := strconv.ParseUint(com1, 10, 16)
		if err != nil {
			return fmt.Errorf("--add-com %s: invalid ASN", com)
		}
		val, err := strconv.ParseUint(com2, 10, 16)
		if err != nil {
			return fmt.Errorf("--add-com %s: invalid VALUE", com)
		}
		s.opt_add_com.Add(uint16(asn), uint16(val))
	}

	// register our callback
	s.P.OnMsg(s.modify, s.Dir, msg.UPDATE)
	return nil
}

func (s *Attr) modify(m *msg.Msg) (accept_message bool) {
	u := &m.Update
	mp := u.ReachMP().Prefixes()

	// handle next-hops
	if s.opt_nexthop4.IsValid() {
		if len(u.Reach) > 0 {
			nh := u.Attrs.Use(attrs.ATTR_NEXTHOP).(*attrs.IP)
			nh.Addr = s.opt_nexthop4
		}

		if mp != nil && mp.IsIPv4() {
			mp.NextHop = s.opt_nexthop4
		}
	}
	if s.opt_nexthop6.IsValid() {
		if mp != nil && mp.IsIPv6() {
			mp.NextHop = s.opt_nexthop6
		}
	}

	// add BGP community attributes
	if todo := s.opt_add_com; len(todo.ASN) > 0 {
		com := u.Attrs.Use(attrs.ATTR_COMMUNITY).(*attrs.Community)
		for i := range todo.ASN {
			com.Add(todo.ASN[i], todo.Value[i])
		}
	}

	return true
}
