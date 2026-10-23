package stages

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/meta"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpipe/core"
)

type Update struct {
	*core.StageBase

	run_nexthop      bool
	opt_nexthop4     netip.Addr
	opt_nexthop6     netip.Addr
	opt_nexthop_self int // 0 = disabled, 1 = prepare, 2 = run
	l_addr           netip.Addr
	r_addr           netip.Addr

	run_com            bool
	opt_add_com        attrs.Community
	opt_drop_com       bool
	opt_add_com_ext    attrs.Extcom
	opt_drop_com_ext   bool
	opt_add_com_large  attrs.LargeCom
	opt_drop_com_large bool
	opt_del_com        [][2]int64 // patterns, -1 = any
	opt_del_com_large  [][3]int64 // patterns, -1 = any

	run_path       bool
	opt_prepend    []uint32
	opt_prepend1st int
	opt_strip_priv bool

	opt_med       int64 // -1 = no change
	opt_drop_med  bool
	opt_lpref     int64 // -1 = no change
	opt_origin    int   // -1 = no change
	opt_drop_attr []attrs.Code
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
	f.String("nexthop4", "", "use given next-hop address for IPv4 prefixes")
	f.String("nexthop6", "", "use given next-hop address for IPv6 prefixes")
	f.Bool("nexthop-self", false, "set next-hop address to our IP address (when possible)")

	f.String("add-com", "", "add given BGP community value")
	f.String("add-com-ext", "", "add given extended BGP community value")
	f.String("add-com-large", "", "add given large BGP community value")
	f.Bool("drop-com", false, "drop the BGP community attribute")
	f.Bool("drop-com-ext", false, "drop the extended BGP community attribute")
	f.Bool("drop-com-large", false, "drop the large BGP community attribute")
	f.StringSlice("del-com", nil, "remove matching communities (CSV, * matches any, eg. 65000:*)")
	f.StringSlice("del-com-large", nil, "remove matching large communities (CSV, eg. 65000:*:*)")

	f.StringSlice("prepend", nil, "prepend given ASN(s) to the AS_PATH (CSV)")
	f.Int("prepend-first", 0, "prepend the first (neighbor) ASN given number of times")
	f.Bool("strip-private", false, "remove private ASNs (RFC 6996) from the AS_PATH")

	f.Int64("set-med", -1, "set MULTI_EXIT_DISC (MED) to given value")
	f.Bool("drop-med", false, "drop the MULTI_EXIT_DISC (MED) attribute")
	f.Int64("set-localpref", -1, "set LOCAL_PREF to given value (iBGP only)")
	f.String("set-origin", "", "set ORIGIN: igp, egp, or incomplete")
	f.StringSlice("drop-attr", nil, "drop given attributes (CSV of names or codes, eg. OTC,28)")

	return s
}

func (s *Update) Attach() error {
	k := s.K

	// next-hop to my IP address?
	if k.Bool("nexthop-self") {
		s.opt_nexthop_self = 1
		s.run_nexthop = true
	}

	// parse --nexthop4
	if val := k.String("nexthop4"); val != "" {
		a, err := netip.ParseAddr(val)
		if err != nil || !a.Is4() {
			return fmt.Errorf("--nexthop4 %s: invalid IPv4 address", val)
		}
		s.opt_nexthop4 = a
		s.run_nexthop = true
	}

	// parse --nexthop6
	if val := k.String("nexthop6"); val != "" {
		a, err := netip.ParseAddr(val)
		if err != nil || !a.Is6() {
			return fmt.Errorf("--nexthop6 %s: invalid IPv6 address", val)
		}
		s.opt_nexthop6 = a
		s.run_nexthop = true
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

	// parse --del-com and --del-com-large
	for _, v := range k.Strings("del-com") {
		p, err := parsePattern(v, 2, 0xffff)
		if err != nil {
			return fmt.Errorf("--del-com %s: %w", v, err)
		}
		s.opt_del_com = append(s.opt_del_com, [2]int64(p))
	}
	for _, v := range k.Strings("del-com-large") {
		p, err := parsePattern(v, 3, 0xffffffff)
		if err != nil {
			return fmt.Errorf("--del-com-large %s: %w", v, err)
		}
		s.opt_del_com_large = append(s.opt_del_com_large, [3]int64(p))
	}

	// should we run BGP community modifications?
	s.run_com = s.opt_add_com.Len() > 0 || s.opt_drop_com ||
		s.opt_add_com_ext.Len() > 0 || s.opt_drop_com_ext ||
		s.opt_add_com_large.Len() > 0 || s.opt_drop_com_large ||
		len(s.opt_del_com) > 0 || len(s.opt_del_com_large) > 0

	// AS_PATH
	for _, v := range k.Strings("prepend") {
		asn, err := strconv.ParseUint(strings.TrimPrefix(strings.ToUpper(v), "AS"), 10, 32)
		if err != nil {
			return fmt.Errorf("--prepend %s: invalid ASN", v)
		}
		s.opt_prepend = append(s.opt_prepend, uint32(asn))
	}
	s.opt_prepend1st = k.Int("prepend-first")
	if s.opt_prepend1st < 0 || s.opt_prepend1st > 32 {
		return fmt.Errorf("--prepend-first must be 0-32")
	}
	s.opt_strip_priv = k.Bool("strip-private")
	s.run_path = len(s.opt_prepend) > 0 || s.opt_prepend1st > 0 || s.opt_strip_priv

	// other attributes
	s.opt_med, s.opt_drop_med = k.Int64("set-med"), k.Bool("drop-med")
	s.opt_lpref = k.Int64("set-localpref")
	if s.opt_med > 0xffffffff || s.opt_lpref > 0xffffffff {
		return fmt.Errorf("--set-med and --set-localpref must fit in 32 bits")
	}
	switch v := strings.ToLower(k.String("set-origin")); v {
	case "":
		s.opt_origin = -1
	case "igp", "egp", "incomplete":
		s.opt_origin = slices.Index([]string{"igp", "egp", "incomplete"}, v)
	default:
		return fmt.Errorf("--set-origin must be igp, egp, or incomplete")
	}
	for _, v := range k.Strings("drop-attr") {
		var ac attrs.Code
		if n, err := strconv.ParseUint(v, 10, 8); err == nil {
			ac = attrs.Code(n)
		} else if err := ac.FromJSON(strings.ToUpper(v)); err != nil {
			return fmt.Errorf("--drop-attr %s: invalid attribute", v)
		}
		s.opt_drop_attr = append(s.opt_drop_attr, ac)
	}

	// register our callback
	s.P.OnMsg(s.modify, s.Dir, msg.UPDATE)
	return nil
}

func (s *Update) modify(m *msg.Msg) (keep_message bool) {
	u := &m.Update

	// NB: pure withdrawals must not carry path attributes (RFC 4271 section 4.3)
	if !u.HasReach() {
		return true
	}

	// modify next-hop?
	if s.run_nexthop {
		m.Edit(s.modifyNexthop(u))
	}

	// modify communities?
	if s.run_com {
		m.Edit(s.modifyCom(u))
	}

	// modify AS_PATH?
	if s.run_path {
		m.Edit(s.modifyPath(u))
	}

	// modify other attributes
	ats := &u.Attrs
	if s.opt_drop_med {
		if ats.Has(attrs.ATTR_MED) {
			ats.Drop(attrs.ATTR_MED)
			m.Edit()
		}
	} else if s.opt_med >= 0 {
		ats.Use(attrs.ATTR_MED).(*attrs.U32).Val = uint32(s.opt_med)
		m.Edit()
	}
	if s.opt_lpref >= 0 {
		ats.Use(attrs.ATTR_LOCALPREF).(*attrs.U32).Val = uint32(s.opt_lpref)
		m.Edit()
	}
	if s.opt_origin >= 0 {
		ats.Use(attrs.ATTR_ORIGIN).(*attrs.Origin).Origin = byte(s.opt_origin)
		m.Edit()
	}
	for _, ac := range s.opt_drop_attr {
		if ats.Has(ac) {
			ats.Drop(ac)
			m.Edit()
		}
	}

	// NB: an edit that grew the UPDATE beyond the negotiated max would reset
	// the session downstream; withdraw its prefixes instead (RFC 7606 style)
	if m.Data == nil {
		maxlen := msg.MAXLEN
		if s.P.Caps.Has(caps.CAP_EXTENDED_MESSAGE) {
			maxlen = msg.MAXLEN_EXT
		}
		if m.Marshal(s.P.Caps) != nil || m.Len() > maxlen {
			s.Warn().Int("len", m.Len()).Msg("UPDATE too long after edits, withdrawing its prefixes")
			reach := u.AllReach()
			u.Reach = nil
			ats.Filter(attrs.ATTR_MP_UNREACH)
			u.AddUnreach(reach...)
			m.Edit()
		}
	}

	return true
}

// modifyPath applies --prepend*, --strip-private to the AS_PATH
func (s *Update) modifyPath(u *msg.Update) (modified bool) {
	ap, ok := u.Attrs.Get(attrs.ATTR_ASPATH).(*attrs.Aspath)
	if !ok {
		return false
	}

	if s.opt_strip_priv {
		for i := range ap.Segments {
			seg := &ap.Segments[i]
			n := len(seg.List)
			seg.List = slices.DeleteFunc(seg.List, isPrivateASN)
			modified = modified || len(seg.List) != n
		}
		ap.Segments = slices.DeleteFunc(ap.Segments, func(seg attrs.AspathSegment) bool { return len(seg.List) == 0 })
	}

	// what to prepend?
	todo := s.opt_prepend
	if n := s.opt_prepend1st; n > 0 && len(ap.Segments) > 0 && !ap.Segments[0].IsSet {
		first := ap.Segments[0].List[0]
		todo = slices.Clone(todo)
		for range n {
			todo = append(todo, first)
		}
	}
	if len(todo) > 0 {
		if len(ap.Segments) == 0 || ap.Segments[0].IsSet {
			ap.Segments = slices.Insert(ap.Segments, 0, attrs.AspathSegment{})
		}
		ap.Segments[0].List = slices.Insert(ap.Segments[0].List, 0, todo...)
		modified = true
	}

	return modified
}

// isPrivateASN returns true for private-use ASNs (RFC 6996)
func isPrivateASN(asn uint32) bool {
	return (asn >= 64512 && asn <= 65534) || (asn >= 4200000000 && asn <= 4294967294)
}

// parsePattern parses n colon-separated values, where * means any (-1)
func parsePattern(v string, n int, max uint64) ([]int64, error) {
	d := strings.Split(v, ":")
	if len(d) != n {
		return nil, fmt.Errorf("need %d colon-separated values", n)
	}
	p := make([]int64, n)
	for i := range d {
		if d[i] == "*" {
			p[i] = -1
		} else if x, err := strconv.ParseUint(d[i], 10, 32); err != nil || x > max {
			return nil, fmt.Errorf("invalid value: %s", d[i])
		} else {
			p[i] = int64(x)
		}
	}
	return p, nil
}

// match returns true iff vals match pattern p
func match(p []int64, vals ...uint32) bool {
	for i := range p {
		if p[i] >= 0 && p[i] != int64(vals[i]) {
			return false
		}
	}
	return true
}

func (s *Update) modifyNexthop(u *msg.Update) (modified bool) {
	// start with the values of the --nexthop4 and --nexthop6 options (can be invalid)
	nexthop4, nexthop6 := s.opt_nexthop4, s.opt_nexthop6

	// need to initialize nexthop-self first?
	// NB: we do this here (not earlier) because we need to wait for
	// the connection to be established before we can get the addresses
	if s.opt_nexthop_self == 1 {
		kv := s.P.KV
		if v, ok := kv.Load("L_LOCAL_ADDR"); ok {
			s.l_addr, _ = v.(netip.Addr)
		}
		if v, ok := kv.Load("R_LOCAL_ADDR"); ok {
			s.r_addr, _ = v.(netip.Addr)
		}
		if s.l_addr.IsValid() || s.r_addr.IsValid() {
			s.opt_nexthop_self = 2 // enable the section below
		} else {
			s.opt_nexthop_self = 0 // permanently disable
		}
	}

	// attempt nexthop-self?
	if s.opt_nexthop_self == 2 {
		var selfip netip.Addr
		if u.Msg.Dir == meta.DIR_L {
			selfip = s.l_addr
		} else {
			selfip = s.r_addr
		}

		// should override --nexthop4 or --nexthop6?
		if !nexthop4.IsValid() && selfip.Is4() {
			nexthop4 = selfip
		}
		if !nexthop6.IsValid() && selfip.Is6() {
			nexthop6 = selfip
		}
	}

	// finally, update next-hops
	mp := u.ReachMP().Prefixes()

	if nexthop4.IsValid() {
		if len(u.Reach) > 0 {
			nh := u.Attrs.Use(attrs.ATTR_NEXTHOP).(*attrs.IP)
			nh.Addr = nexthop4
			modified = true
		}

		if mp != nil && mp.IsIPv4() {
			mp.NextHop = nexthop4
			modified = true
		}
	}

	if nexthop6.IsValid() {
		if mp != nil && mp.IsIPv6() {
			mp.NextHop = nexthop6
			modified = true
		}
	}

	return modified
}

func (s *Update) modifyCom(u *msg.Update) (modified bool) {
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

	if c, ok := u.Attrs.Get(attrs.ATTR_COMMUNITY).(*attrs.Community); ok && len(s.opt_del_com) > 0 {
		j := 0
		for i := range c.ASN {
			if !slices.ContainsFunc(s.opt_del_com, func(p [2]int64) bool {
				return match(p[:], uint32(c.ASN[i]), uint32(c.Value[i]))
			}) {
				c.ASN[j], c.Value[j] = c.ASN[i], c.Value[i]
				j++
			}
		}
		if j != len(c.ASN) {
			c.ASN, c.Value = c.ASN[:j], c.Value[:j]
			if j == 0 {
				u.Attrs.Drop(attrs.ATTR_COMMUNITY)
			}
			modified = true
		}
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

	if c, ok := u.Attrs.Get(attrs.ATTR_LARGE_COMMUNITY).(*attrs.LargeCom); ok && len(s.opt_del_com_large) > 0 {
		j := 0
		for i := range c.ASN {
			if !slices.ContainsFunc(s.opt_del_com_large, func(p [3]int64) bool {
				return match(p[:], c.ASN[i], c.Value1[i], c.Value2[i])
			}) {
				c.ASN[j], c.Value1[j], c.Value2[j] = c.ASN[i], c.Value1[i], c.Value2[i]
				j++
			}
		}
		if j != len(c.ASN) {
			c.ASN, c.Value1, c.Value2 = c.ASN[:j], c.Value1[:j], c.Value2[:j]
			if j == 0 {
				u.Attrs.Drop(attrs.ATTR_LARGE_COMMUNITY)
			}
			modified = true
		}
	}

	return modified
}
