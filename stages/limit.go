package stages

import (
	"fmt"
	"net/netip"
	"slices"
	"sync"

	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/bgpfix/bgpfix/af"
	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	bgpipe "github.com/bgpfix/bgpipe/core"
	"github.com/puzpuzpuz/xsync/v3"
)

type Limit struct {
	*bgpipe.StageBase

	afs  map[af.AF]bool // address families to consider
	ipv4 bool           // consider IPv4
	ipv6 bool           // consider IPv6

	v6spec bool // blindly accept too-specific IPv6 prefixes?
	minlen int  // max prefix length
	maxlen int  // max prefix length

	permanent bool // do not consider withdrawals?

	count        int64 // max global prefix count
	count_origin int64 // max prefix count for single origin
	count_block  int64 // max prefix count for IP block

	global *limitDb                       // global db
	origin *xsync.MapOf[uint32, *limitDb] // per-origin db
}

const (
	MAX_IPV6 = 58
)

func NewLimit(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s  = &Limit{StageBase: parent}
		so = &s.Options
		sf = so.Flags
	)

	sf.BoolP("ipv4", "4", false, "process IPv4 prefixes")
	sf.BoolP("ipv6", "6", false, "process IPv6 prefixes")
	sf.Bool("multicast", false, "process multicast prefixes")
	sf.Bool("permanent", false, "make announcements permanent (do not consider withdrawals)")

	sf.IntP("max-length", "l", 0, "max. prefix length (0 = no limit)")
	sf.IntP("min-length", "m", 0, "min. prefix length (0 = no limit)")
	sf.Bool("v6spec", false, "blindly accept too-specific IPv6 prefixes (>/58)")

	sf.IntP("count", "c", 0, "global count limit (0 = no limit)")
	sf.IntP("origin", "o", 0, "per-AS origin limit (0 = no limit)")
	sf.IntP("block", "b", 0, "per-IP block limit (0 = no limit)")
	sf.IntP("block-length", "B", 0, "IP block length (0 = /8 for v4, or /32 for v6)")

	sf.StringSliceP("kill", "k", []string{"count"}, "kill the session on these events")

	so.Descr = "limit prefix lengths and counts"
	so.Usage = "limit [OPTIONS]"

	so.Events = map[string]string{
		"specific": "too specific IPv6 prefix announced",
		"length":   "too long (or too short) prefix announced",
		"count":    "too many prefixes reachable over the session",
		"origin":   "too many prefixes for a single AS origin",
		"block":    "too many prefixes for a single IP block",
	}

	so.Bidir = true // will aggregate both directions

	s.afs = make(map[af.AF]bool)
	s.global = s.newDb()
	s.origin = xsync.NewMapOf[uint32, *limitDb]()

	return s
}

func (s *Limit) Attach() error {
	k := s.K

	// address family
	s.ipv4 = k.Bool("ipv4")
	s.ipv6 = k.Bool("ipv6")
	if !s.ipv4 && !s.ipv6 {
		s.ipv4 = true // by default, IPv4 only
	}
	if s.ipv4 {
		s.afs[af.New(af.AFI_IPV4, af.SAFI_UNICAST)] = true
		if k.Bool("multicast") {
			s.afs[af.New(af.AFI_IPV4, af.SAFI_MULTICAST)] = true
		}
	}
	if s.ipv6 {
		s.afs[af.New(af.AFI_IPV6, af.SAFI_UNICAST)] = true
		if k.Bool("multicast") {
			s.afs[af.New(af.AFI_IPV6, af.SAFI_MULTICAST)] = true
		}
	}

	s.v6spec = k.Bool("v6spec")
	s.minlen = k.Int("min-length")
	if s.minlen < 0 || s.minlen >= 128 {
		return fmt.Errorf("invalid minimum IP prefix length %d", s.maxlen)
	}
	s.maxlen = k.Int("max-length")
	if s.maxlen < 0 || s.maxlen >= 128 {
		return fmt.Errorf("invalid maximum IP prefix length %d", s.maxlen)
	}
	if s.maxlen > 0 && s.minlen > 0 && s.maxlen < s.minlen {
		return fmt.Errorf("maximum IP prefix length lower than minimum IP prefix length")
	}

	s.count = k.Int64("count")
	s.count_origin = k.Int64("origin")
	s.count_block = k.Int64("block")

	s.permanent = k.Bool("permanent")

	s.P.OnMsg(s.onMsg, s.Dir, msg.UPDATE)
	return nil
}

func (s *Limit) onMsg(m *msg.Msg) (action pipe.Action) {
	var rbefore, rafter, ubefore, uafter int

	// process reachable prefixes
	u := &m.Update
	rbefore, rafter = s.checkReach(u)

	// process unreachable prefixes
	if !s.permanent {
		ubefore, uafter = s.checkUnreach(u)
	}

	// need to drop the whole message?
	if rafter+uafter == 0 && rbefore+ubefore > 0 {
		return pipe.ACTION_DROP
	}

	return // accept
}

func (s *Limit) isSpecific(p netip.Prefix) bool {
	if a, l := p.Addr(), p.Bits(); a.Is6() {
		return l > MAX_IPV6
	} else {
		return false
	}
}

func (s *Limit) isLength(p netip.Prefix) bool {
	l := p.Bits()
	if s.minlen > 0 && l < s.minlen {
		return true
	}
	if s.maxlen > 0 && l > s.maxlen {
		return true
	}
	return false
}

// translates IP prefix to uint64 key, assuming prefix length <=/58 for IPv6
func (s *Limit) p2key(p netip.Prefix) (ret uint64) {
	// addr in the top bytes
	for i, b := range p.Addr().AsSlice() {
		if i > 0 {
			ret <<= 8
		}
		ret |= uint64(b)
	}

	// still have space?
	if p.Addr().Is4() {
		ret <<= 8
	}

	// prefix length in the bottom 6 bits
	m := uint64(0b111111)
	ret &= ^m
	ret |= uint64(p.Bits()) & m

	return ret
}

func (s *Limit) checkReach(u *msg.Update) (before, after int) {
	// drops p from u if violates the rules
	dropPrefix := func(p netip.Prefix) (drop bool) {
		defer func() {
			if drop {
				u.Msg.Dirty = true
			}
		}()

		// too specific?
		if s.isSpecific(p) {
			if s.v6spec {
				return false // take it as-is
			}
			s.Event("specific", p.String()) //, u.Msg)
			return true
		}

		// too long?
		if s.isLength(p) {
			s.Event("length", p.String()) //, u.Msg)
			return true
		}

		// is over global limit?
		key := s.p2key(p)
		if s.isOver(key, s.global, s.count) {
			s.Event("count", p.String())
			// TODO: kill session?
			return true
		}

		// TODO: per-block limit
		// TODO: per-origin limit

		return false
	}

	// prefixes in the non-MP IPv4 part?
	if s.ipv4 {
		before += len(u.Reach)
		u.Reach = slices.DeleteFunc(u.Reach, dropPrefix)
		after += len(u.Reach)
	}

	// prefixes in the MP part?
	if mp := u.Attrs.MPPrefixes(attrs.ATTR_MP_REACH); mp != nil && s.afs[mp.AF] {
		before += len(mp.Prefixes)
		mp.Prefixes = slices.DeleteFunc(mp.Prefixes, dropPrefix)
		after += len(mp.Prefixes)

		// anything left?
		if len(mp.Prefixes) == 0 {
			u.Attrs.Drop(attrs.ATTR_MP_REACH)
		}
	}

	return before, after
}

func (s *Limit) checkUnreach(u *msg.Update) (before, after int) {
	// helper: check prefix length
	checkLen := func(p netip.Prefix) bool {
		if s.isSpecific(p) {
			if s.v6spec {
				return false // take it as-is
			}
			u.Msg.Dirty = true
			return true
		}
		if s.isLength(p) {
			u.Msg.Dirty = true
			return true
		}
		return false
	}

	// prefixes in the non-MP IPv4 part?
	if s.ipv4 {
		before += len(u.Unreach)
		u.Unreach = slices.DeleteFunc(u.Unreach, checkLen)
		after += len(u.Reach)
	}

	// prefixes in the MP part?
	if mp := u.Attrs.MPPrefixes(attrs.ATTR_MP_UNREACH); mp != nil && s.afs[mp.AF] {
		before += len(mp.Prefixes)
		mp.Prefixes = slices.DeleteFunc(mp.Prefixes, checkLen)
		after += len(mp.Prefixes)

		// anything left?
		if len(mp.Prefixes) == 0 {
			u.Attrs.Drop(attrs.ATTR_MP_UNREACH)
		}
	}

	return before, after
}

type limitDb struct {
	sync.Mutex
	count int64
	db    *roaring64.Bitmap
}

func (s *Limit) newDb() *limitDb {
	return &limitDb{
		db: roaring64.New(),
	}
}

func (s *Limit) isOver(key uint64, lp *limitDb, limit int64) bool {
	if lp == nil || limit == 0 {
		return false
	}
	lp.Lock()
	defer lp.Unlock()

	// already seen?
	if lp.db.Contains(key) {

		return false
	}

	// already at the limit? drop it
	if limit > 0 && lp.count >= limit {
		return true
	}

	// add the new prefix
	lp.count++
	lp.db.Add(key)

	// ok, new prefix but still under the limit
	return false
}
