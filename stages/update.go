package stages

import (
	"fmt"
	"net/netip"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpipe/core"
)

type Update struct {
	*core.StageBase

	opt_nexthop4       netip.Addr
	opt_nexthop6       netip.Addr
	opt_add_com        attrs.Community
	opt_drop_com       bool
	opt_add_com_ext    attrs.Extcom
	opt_drop_com_ext   bool
	opt_add_com_large  attrs.LargeCom
	opt_drop_com_large bool
}

func NewUpdate(parent *core.StageBase) core.Stage {
	var (
		s = &Update{StageBase: parent}
		o = &s.Options
	)

	o.Descr = "modify UPDATE messages"
	o.FilterIn = true
	o.Bidir = true

	f := o.Flags
	f.String("set-nexthop4", "", "use given next-hop address for IPv4 prefixes")
	f.String("set-nexthop6", "", "use given next-hop address for IPv6 prefixes")

	f.String("add-com", "", "add given BGP community value")
	f.String("add-com-ext", "", "add given extended BGP community value")
	f.String("add-com-large", "", "add given large BGP community value")
	f.Bool("drop-com", false, "drop the BGP community attribute")
	f.Bool("drop-com-ext", false, "drop the extended BGP community attribute")
	f.Bool("drop-com-large", false, "drop the large BGP community attribute")

	return s
}

func (s *Update) Attach() error {
	k := s.K

	// parse --set-nexthop4
	if val := k.String("set-nexthop4"); val != "" {
		a, err := netip.ParseAddr(val)
		if err != nil || !a.Is4() {
			return fmt.Errorf("--nexthop4 %s: invalid IPv4 address", val)
		}
		s.opt_nexthop4 = a
	}

	// parse --set-nexthop6
	if val := k.String("set-nexthop6"); val != "" {
		a, err := netip.ParseAddr(val)
		if err != nil || !a.Is6() {
			return fmt.Errorf("--nexthop6 %s: invalid IPv6 address", val)
		}
		s.opt_nexthop6 = a
	}

	// parse --add-com
	if val := k.String("add-com"); val != "" {
		if val[0] != '[' {
			val = fmt.Sprintf("[ %v ]", val)
		}
		err := s.opt_add_com.FromJSON([]byte(val))
		if err != nil {
			return fmt.Errorf("--add-com %s: %w", val, err)
		}
	}
	s.opt_drop_com = k.Bool("drop-com")

	// parse --add-com-ext
	if val := k.String("add-com-ext"); val != "" {
		if val[0] != '[' {
			val = fmt.Sprintf("[ %v ]", val)
		}
		err := s.opt_add_com_ext.FromJSON([]byte(val))
		if err != nil {
			return fmt.Errorf("--add-com-ext %s: %w", val, err)
		}
	}
	s.opt_drop_com_ext = k.Bool("drop-com-ext")

	// parse --add-com-large
	if val := k.String("add-com-large"); val != "" {
		if val[0] != '[' {
			val = fmt.Sprintf("[ %v ]", val)
		}
		err := s.opt_add_com_large.FromJSON([]byte(val))
		if err != nil {
			return fmt.Errorf("--add-com-large %s: %w", val, err)
		}
	}
	s.opt_drop_com_large = k.Bool("drop-com-large")

	// register our callback
	s.P.OnMsg(s.modify, s.Dir, msg.UPDATE)
	return nil
}

func (s *Update) modify(m *msg.Msg) (accept_message bool) {
	u := &m.Update
	modified := false

	// handle next-hops
	mp := u.ReachMP().Prefixes()
	if s.opt_nexthop4.IsValid() {
		if len(u.Reach) > 0 {
			nh := u.Attrs.Use(attrs.ATTR_NEXTHOP).(*attrs.IP)
			nh.Addr = s.opt_nexthop4
			modified = true
		}

		if mp != nil && mp.IsIPv4() {
			mp.NextHop = s.opt_nexthop4
			modified = true
		}
	}
	if s.opt_nexthop6.IsValid() {
		if mp != nil && mp.IsIPv6() {
			mp.NextHop = s.opt_nexthop6
			modified = true
		}
	}

	// BGP communities
	if s.opt_drop_com {
		u.Attrs.Drop(attrs.ATTR_COMMUNITY)
		modified = true
	} else if todo := s.opt_add_com; todo.Len() > 0 {
		com := u.Attrs.Use(attrs.ATTR_COMMUNITY).(*attrs.Community)
		for i := range todo.Len() {
			com.Add(todo.ASN[i], todo.Value[i])
		}
		modified = true
	}
	if s.opt_drop_com_ext {
		u.Attrs.Drop(attrs.ATTR_EXT_COMMUNITY)
		modified = true
	} else if todo := s.opt_add_com_ext; todo.Len() > 0 {
		com := u.Attrs.Use(attrs.ATTR_EXT_COMMUNITY).(*attrs.Extcom)
		for i := range todo.Len() {
			com.Add(todo.Type[i], todo.Value[i])
		}
		modified = true
	}
	if s.opt_drop_com_large {
		u.Attrs.Drop(attrs.ATTR_LARGE_COMMUNITY)
		modified = true
	} else if todo := s.opt_add_com_large; todo.Len() > 0 {
		com := u.Attrs.Use(attrs.ATTR_LARGE_COMMUNITY).(*attrs.LargeCom)
		for i := range todo.Len() {
			com.Add(todo.ASN[i], todo.Value1[i], todo.Value2[i])
		}
		modified = true
	}

	// have we actually modified the message?
	if modified {
		m.Modified()
	}

	return true
}
