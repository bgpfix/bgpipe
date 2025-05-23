/*
 * limit: implement per-session, per-IP-block and per-AS-origin limits on prefix counts
 *
 * Released as part of the Kirin paper (Prehn, Foremski, Gasser).
 *
 * License: MIT
 * Author: Pawel Foremski <pjf@foremski.pl, Nov 2023
 */

package stages

import (
	"encoding/binary"
	"fmt"
	"slices"
	"sync"

	"github.com/bgpfix/bgpfix/afi"
	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpipe/core"
	"github.com/puzpuzpuz/xsync/v3"
)

type Limit struct {
	*core.StageBase

	afs  map[afi.AS]bool // address families to consider
	ipv4 bool            // consider IPv4
	ipv6 bool            // consider IPv6

	minlen int // max prefix length
	maxlen int // max prefix length
	blen4  int // IPv4 block length
	blen6  int // IPv6 block length

	permanent bool // do not consider withdrawals?

	limit_session int64 // max global prefix count
	limit_origin  int64 // max prefix count for single origin
	limit_block   int64 // max prefix count for IP block

	session *xsync.MapOf[nlri.NLRI, *limitPrefix] // session db
	origin  *xsync.MapOf[uint32, *limitCounter]   // per-origin db
	block   *xsync.MapOf[uint64, *limitCounter]   // per-block db
}

func NewLimit(parent *core.StageBase) core.Stage {
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
	sf.IntP("block-length", "B", 0, "IP block length (max. 64, 0 = 8/32 for v4/v6)")

	so.Descr = "limit prefix lengths and counts"

	so.Events = map[string]string{
		"long":   "too long prefix announced",
		"short":  "too short prefix announced",
		"count":  "too many prefixes reachable over the session",
		"origin": "too many prefixes for a single AS origin",
		"block":  "too many prefixes for a single IP block",
	}

	so.Bidir = true // will aggregate both directions
	so.FilterIn = true

	s.afs = make(map[afi.AS]bool)
	s.session = xsync.NewMapOf[nlri.NLRI, *limitPrefix]()
	s.origin = xsync.NewMapOf[uint32, *limitCounter]()
	s.block = xsync.NewMapOf[uint64, *limitCounter]()

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
		s.afs[afi.AS_IPV4_UNICAST] = true
		if k.Bool("multicast") {
			s.afs[afi.AS_IPV4_MULTICAST] = true
		}
	}
	if s.ipv6 {
		s.afs[afi.AS_IPV6_UNICAST] = true
		if k.Bool("multicast") {
			s.afs[afi.AS_IPV6_MULTICAST] = true
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
	if s.blen6 < 0 || s.blen6 > 64 {
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

func (s *Limit) onMsg(m *msg.Msg) bool {
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
		return false
	}

	// limits OK, take it
	return true
}

func (s *Limit) isShort(p nlri.NLRI) bool {
	return s.minlen > 0 && p.Bits() < s.minlen
}

func (s *Limit) isLong(p nlri.NLRI) bool {
	return s.maxlen > 0 && p.Bits() > s.maxlen
}

// translates IP prefix to IP block, assuming prefix length <=/64
func (s *Limit) p2b(p nlri.NLRI) uint64 {
	b := p.Addr().AsSlice()
	switch len(b) {
	case 4:
		val := uint64(binary.BigEndian.Uint32(b))
		bitmask := ^(uint64(1)<<(32-s.blen4) - 1)
		return val & bitmask
	case 16:
		val := binary.BigEndian.Uint64(b) // ignore bottom 64 bits
		bitmask := ^(uint64(1)<<(64-s.blen6) - 1)
		return val & bitmask
	default:
		return 0
	}
}

func (s *Limit) checkReach(u *msg.Update) (before, after int) {
	origin := u.AsPath().Origin()

	// drops p from u if violates the rules
	dropReach := func(p nlri.NLRI) (drop bool) {
		defer func() { u.Msg.Edit(drop) }()

		// too long or short?
		if s.isShort(p) {
			s.Event("short", p.String(), origin)
			return true
		} else if s.isLong(p) {
			s.Event("long", p.String(), origin)
			return true
		}

		// get pp
	retry:
		pp, loaded := s.session.LoadOrCompute(p, newLimitPrefix)
		pp.Lock()
		if pp.dropped {
			pp.Unlock()
			goto retry
		}

		// cleanup on exit
		defer func() {
			if drop && !loaded {
				pp.dropped = true
				s.session.Delete(p) // revert what we added
			}
			pp.Unlock()
		}()

		// check AS origin limit
		if s.limit_origin > 0 && slices.Index(pp.origins, origin) < 0 {
			po, _ := s.origin.LoadOrCompute(origin, newLimitCounter)
			po.Lock()
			defer po.Unlock()

			// can't add more to origin?
			if po.counter >= s.limit_origin {
				s.Event("origin", p.String(), origin, po.counter)
				return true
			}

			// add to origin iff other checks ok
			defer func() {
				if !drop {
					pp.origins = append(pp.origins, origin)
					po.counter++
				}
			}()
		}

		// check IP block limit
		if s.limit_block > 0 && !loaded {
			pb, _ := s.block.LoadOrCompute(s.p2b(p), newLimitCounter)
			pb.Lock()
			defer pb.Unlock()

			// can't add more to block?
			if pb.counter >= s.limit_block {
				s.Event("block", p.String(), origin, pb.counter)
				return true
			}

			// add to block iff other checks ok
			defer func() {
				if !drop {
					pb.counter++
				}
			}()
		}

		// check session limit
		if s.limit_session > 0 && !loaded {
			// can't add more to session?
			if size := s.session.Size(); int64(size) >= s.limit_session {
				s.Event("session", p.String(), origin, size)
				return true
			}
		}

		// accept the prefix
		pp.accepted = true
		return false
	}

	// prefixes in the non-MP IPv4 part?
	if s.ipv4 {
		before += len(u.Reach)
		u.Reach = slices.DeleteFunc(u.Reach, dropReach)
		after += len(u.Reach)
	}

	// prefixes in the MP part?
	if mp := u.ReachMP().Prefixes(); mp != nil && s.afs[mp.AS] {
		before += len(mp.Prefixes)
		mp.Prefixes = slices.DeleteFunc(mp.Prefixes, dropReach)
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
	dropUnreach := func(p nlri.NLRI) (drop bool) {
		// too long or short?
		if s.isShort(p) || s.isLong(p) {
			u.Msg.Edit() // TODO: sure? why not check drop? FIXME
			return true
		}

		// remove from session
		pp, ok := s.session.LoadAndDelete(p)
		if pp == nil || !ok {
			return false // nothing to do
		}

		// wait for lock
		pp.Lock()
		defer func() {
			pp.dropped = true
			pp.Unlock()
		}()

		// done already?
		if pp.dropped || !pp.accepted {
			return false
		}

		// remove from origins
		if s.limit_origin > 0 {
			for _, origin := range pp.origins {
				po, ok := s.origin.Load(origin)
				if ok && po != nil {
					po.Lock()
					po.counter--
					po.Unlock()
				}
			}
		}

		// remove from IP block
		if s.limit_block > 0 {
			if pb, ok := s.block.Load(s.p2b(p)); ok && pb != nil {
				pb.Lock()
				pb.counter--
				pb.Unlock()
			}
		}

		return false
	}

	// prefixes in the non-MP IPv4 part?
	if s.ipv4 {
		before += len(u.Unreach)
		u.Unreach = slices.DeleteFunc(u.Unreach, dropUnreach)
		after += len(u.Reach)
	}

	// prefixes in the MP part?
	if mp := u.UnreachMP().Prefixes(); mp != nil && s.afs[mp.AS] {
		before += len(mp.Prefixes)
		mp.Prefixes = slices.DeleteFunc(mp.Prefixes, dropUnreach)
		after += len(mp.Prefixes)

		// anything left?
		if len(mp.Prefixes) == 0 {
			u.Attrs.Drop(attrs.ATTR_MP_UNREACH)
		}
	}

	return before, after
}

type limitPrefix struct {
	sync.Mutex
	accepted bool
	dropped  bool
	origins  []uint32
}

func newLimitPrefix() *limitPrefix {
	return &limitPrefix{
		origins: make([]uint32, 0, 1),
	}
}

type limitCounter struct {
	sync.Mutex
	counter int64
}

func newLimitCounter() *limitCounter {
	return &limitCounter{}
}
