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

	minlen int // max prefix length
	maxlen int // max prefix length
	blen4  int // IPv4 block length
	blen6  int // IPv6 block length

	permanent bool // do not consider withdrawals?

	limit_session int64 // max global prefix count
	limit_origin  int64 // max prefix count for single origin
	limit_block   int64 // max prefix count for IP block

	session *limitDb                       // global db
	origin  *xsync.MapOf[uint32, *limitDb] // per-origin db
	block   *xsync.MapOf[uint64, *limitDb] // per-block db
}

const (
	MAX_IPV6 = 58 // 58 bits for prefix + 6 bits for length
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

	sf.IntP("min-length", "m", 0, "min. prefix length (0 = no limit)")
	sf.IntP("max-length", "M", 0, "max. prefix length (0 = no limit)")
	sf.IntP("session", "s", 0, "global session limit (0 = no limit)")
	sf.IntP("origin", "o", 0, "per-AS origin limit (0 = no limit)")
	sf.IntP("block", "b", 0, "per-IP block limit (0 = no limit)")
	sf.IntP("block-length", "B", 0, "IP block length (max. 56, 0 = 8/32 for v4/v6)")

	so.Descr = "limit prefix lengths and counts"
	so.Usage = "limit [OPTIONS]"

	so.Events = map[string]string{
		"long":   "too long prefix announced",
		"short":  "too short prefix announced",
		"count":  "too many prefixes reachable over the session",
		"origin": "too many prefixes for a single AS origin",
		"block":  "too many prefixes for a single IP block",
	}

	so.Bidir = true // will aggregate both directions

	s.afs = make(map[af.AF]bool)
	s.session = s.newDb()
	s.origin = xsync.NewMapOf[uint32, *limitDb]()
	s.block = xsync.NewMapOf[uint64, *limitDb]()

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

	s.limit_session = k.Int64("session")
	s.limit_origin = k.Int64("origin")
	s.limit_block = k.Int64("block")

	s.blen6 = k.Int("block-length")
	if s.blen6 < 0 || s.blen6 >= MAX_IPV6 {
		return fmt.Errorf("invalid IP block length %d", s.blen6)
	}
	if s.blen6 == 0 {
		s.blen4 = 16
		s.blen6 = 32
	} else {
		s.blen4 = min(32, s.blen6)
	}

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

	return pipe.ACTION_OK
}

func (s *Limit) isShort(p netip.Prefix) bool {
	return s.minlen > 0 && p.Bits() < s.minlen
}

func (s *Limit) isLong(p netip.Prefix) bool {
	return s.maxlen > 0 && p.Bits() > s.maxlen
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
	origin := u.Attrs.AsOrigin()

	// drops p from u if violates the rules
	dropPrefix := func(p netip.Prefix) (drop bool) {
		defer func() {
			if drop {
				u.Msg.Dirty = true
			}
		}()

		// too long or short?
		if s.isShort(p) {
			s.Event("short", p.String(), origin)
			return true
		} else if s.isLong(p) {
			s.Event("long", p.String(), origin)
			return true
		}

		// is over per-origin limit?
		if s.limit_origin > 0 {
			db, _ := s.origin.LoadOrCompute(origin, s.newDb)
			count, isover := s.dbAdd(db, p, s.limit_origin)
			if isover {
				s.Event("origin", p.String(), origin, count)
				return true
			}
		}

		// is over per-block limit?
		if s.limit_block > 0 {
			var block netip.Prefix
			if p.Addr().Is6() {
				block, _ = p.Addr().Prefix(s.blen6)
			} else {
				block, _ = p.Addr().Prefix(s.blen4)
			}

			db, _ := s.block.LoadOrCompute(s.p2key(block), s.newDb)
			count, isover := s.dbAdd(db, p, s.limit_block)
			if isover {
				s.Event("block", p.String(), origin, count, block.String())
				return true
			}
		}

		// is over session limit?
		if s.limit_session > 0 {
			count, isover := s.dbAdd(s.session, p, s.limit_session)
			if isover {
				s.Event("session", p.String(), origin, count)
				return true
			}
		}

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
	// drops p from u if violates the rules
	dropPrefix := func(p netip.Prefix) (drop bool) {
		// too long or short?
		if s.isShort(p) || s.isLong(p) {
			u.Msg.Dirty = true
			return true
		}

		// per-origin limit?
		if s.limit_origin > 0 {
			// FIXME: there's no such thing as origin for withdrawals
			origin := u.Attrs.AsOrigin()
			db, _ := s.origin.Load(origin)
			s.dbDel(db, p)
		}

		// per-block limit?
		if s.limit_block > 0 {
			var block netip.Prefix
			if p.Addr().Is6() {
				block, _ = p.Addr().Prefix(s.blen6)
			} else {
				block, _ = p.Addr().Prefix(s.blen4)
			}

			db, _ := s.block.Load(s.p2key(block))
			s.dbDel(db, p)
		}

		// session limit?
		if s.limit_session > 0 {
			s.dbDel(s.session, p)
		}

		return false
	}

	// prefixes in the non-MP IPv4 part?
	if s.ipv4 {
		before += len(u.Unreach)
		u.Unreach = slices.DeleteFunc(u.Unreach, dropPrefix)
		after += len(u.Reach)
	}

	// prefixes in the MP part?
	if mp := u.Attrs.MPPrefixes(attrs.ATTR_MP_UNREACH); mp != nil && s.afs[mp.AF] {
		before += len(mp.Prefixes)
		mp.Prefixes = slices.DeleteFunc(mp.Prefixes, dropPrefix)
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
	db    map[netip.Prefix]struct{}
	roar  *roaring64.Bitmap
}

func (s *Limit) newDb() *limitDb {
	return &limitDb{}
}

func (s *Limit) dbAdd(lp *limitDb, p netip.Prefix, limit int64) (count int64, isover bool) {
	if lp == nil || limit == 0 {
		return 0, false
	}
	lp.Lock()
	defer lp.Unlock()

	// where to look for p?
	indb := lp.roar == nil || p.Bits() > MAX_IPV6

	// already seen?
	var key uint64
	if indb {
		if lp.db != nil {
			if _, ok := lp.db[p]; ok {
				return lp.count, false
			}
		} else {
			lp.db = make(map[netip.Prefix]struct{})
		}
	} else {
		key = s.p2key(p)
		if lp.roar.Contains(key) {
			return lp.count, false
		}
	}

	// already at the limit? drop it
	if limit > 0 && lp.count >= limit {
		return lp.count, true
	}

	// add the new prefix
	lp.count++
	if !indb {
		lp.roar.Add(key)
	} else {
		lp.db[p] = struct{}{}

		// lazy rewrite to roar?
		if lp.roar == nil && len(lp.db) > 10 {
			lp.roar = roaring64.New()
			for p2 := range lp.db {
				if p2.Bits() <= MAX_IPV6 {
					lp.roar.Add(s.p2key(p2))
					delete(lp.db, p2)
				}
			}
			if len(lp.db) == 0 {
				lp.db = nil
			}
		}
	}

	// ok, new prefix but still under the limit
	return lp.count, false
}

func (s *Limit) dbDel(lp *limitDb, p netip.Prefix) {
	if lp == nil {
		return
	}
	lp.Lock()
	defer lp.Unlock()

	if lp.roar == nil || p.Bits() > MAX_IPV6 {
		if lp.db != nil {
			if _, ok := lp.db[p]; ok {
				delete(lp.db, p)
				lp.count--
			}
		}
	} else {
		if lp.roar.CheckedRemove(s.p2key(p)) {
			lp.count--
		}
	}
}
