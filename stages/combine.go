package stages

import (
	"net/netip"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/afi"
	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

// Combine merges UPDATEs from the same peer, with the same attributes, into one UPDATE.
// NB: takes all messages and sends them again through s.in, to keep their order
type Combine struct {
	*core.StageBase
	in *pipe.Input

	window  time.Duration // max. time to hold an UPDATE
	maxsize int           // max. size of a combined UPDATE

	mu    sync.Mutex
	peers map[peer]*combinePeer // pending UPDATEs, by peer
}

// combinePeer holds the pending UPDATEs of one peer
type combinePeer struct {
	time   time.Time                // when the first UPDATE arrived
	groups []*combineGroup          // in order of arrival
	bykey  map[string]*combineGroup // by attributes
	pfx    map[nlri.Prefix]bool     // all pending prefixes
}

// combineGroup is one combined UPDATE: the first one, plus the prefixes of the next ones
type combineGroup struct {
	m    *msg.Msg
	add  []nlri.Prefix
	size int // estimated size of m
}

func NewCombine(parent *core.StageBase) core.Stage {
	s := &Combine{StageBase: parent, peers: make(map[peer]*combinePeer)}

	o := &s.Options
	o.Descr = "combine UPDATEs with the same attributes into one"
	o.IsProducer = true
	o.FilterIn = true
	o.StopUpstream = true

	f := o.Flags
	f.DurationP("window", "w", time.Second, "max. time to hold an UPDATE")
	f.Int("max-size", msg.MAXLEN, "max. size of a combined UPDATE (bytes)")

	return s
}

func (s *Combine) Attach() error {
	s.window = s.K.Duration("window")
	if s.window <= 0 {
		return s.Errorf("--window must be positive")
	}
	s.maxsize = s.K.Int("max-size")
	if s.maxsize < 64 {
		return s.Errorf("--max-size too small")
	}

	s.in = s.P.AddInput(s.Dir)
	s.P.OnMsg(s.onMsg, s.Dir)
	return nil
}

func (s *Combine) Run() error {
	ticker := time.NewTicker(max(s.window/4, 10*time.Millisecond))
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			s.flush(now.Add(-s.window))
		case <-s.Ctx.Done():
			s.flush(time.Time{})
			return nil
		}
	}
}

// onMsg takes m into a pending UPDATE, or sends it right after the pending ones
func (s *Combine) onMsg(m *msg.Msg) bool {
	peer := peerKey(m)
	key, pfx := s.classify(m)

	s.mu.Lock()
	defer s.mu.Unlock()

	// can't combine m? (eg. End-of-RIB, KEEPALIVE)
	cp := s.peers[peer]
	if key == "" {
		s.emit(peer, cp)
		pipe.Take(m)
		s.in.WriteMsg(m)
		return false
	}

	// a prefix already pending? keep the order of its changes
	for _, p := range pfx {
		if cp != nil && cp.pfx[p] {
			s.emit(peer, cp)
			cp = nil
		}
	}
	if cp == nil {
		cp = &combinePeer{time: time.Now(), bykey: make(map[string]*combineGroup), pfx: make(map[nlri.Prefix]bool)}
		s.peers[peer] = cp
	}
	for _, p := range pfx {
		cp.pfx[p] = true
	}

	// add to the pending UPDATE, if it fits; drop m
	size := 0
	for _, p := range pfx {
		size += 1 + (p.Bits()+7)/8
		if p.Add != nil {
			size += 4
		}
	}
	if g := cp.bykey[key]; g != nil && g.size+size <= s.maxsize {
		g.add = append(g.add, pfx...)
		g.size += size
		return false
	}

	// start a new pending UPDATE with m
	if m.Marshal(s.P.Caps) != nil {
		return false
	}
	pipe.Take(m)
	g := &combineGroup{m: m, size: m.Len() + 1} // +1: MP attributes may need the extended length
	cp.groups = append(cp.groups, g)
	cp.bykey[key] = g
	return false
}

// classify returns the attributes key and prefixes of m, or an empty key if m can't be combined
func (s *Combine) classify(m *msg.Msg) (key string, pfx []nlri.Prefix) {
	pfx, reach := unicastPrefixes(m)
	if len(pfx) == 0 {
		return "", nil
	}
	buf := []byte{'W', '4'}
	if reach {
		buf[0] = 'A'
	}
	if pfx[0].Addr().Is6() {
		buf[1] = '6'
	}
	if !reach {
		return string(buf), pfx
	}

	// the next-hop and attributes, without MP_REACH
	if mp := m.Update.ReachMP().Prefixes(); mp != nil {
		buf = append(buf, mp.NextHop.AsSlice()...)
		buf = append(buf, mp.LinkLocal.AsSlice()...)
	}
	m.Update.Attrs.Each(func(_ int, ac attrs.Code, at attrs.Attr) {
		if ac != attrs.ATTR_MP_REACH {
			buf = at.Marshal(buf, s.P.Caps, &m.Meta)
		}
	})
	return string(buf), pfx
}

// unicastPrefixes returns the prefixes of m and if they are announced, iff m is an UPDATE
// that only announces, or only withdraws, IPv4 or IPv6 unicast prefixes
func unicastPrefixes(m *msg.Msg) (pfx []nlri.Prefix, reach bool) {
	if m.Type != msg.UPDATE {
		return nil, false
	}
	u := &m.Update
	mpr, mpu := u.ReachMP(), u.UnreachMP()
	switch {
	case (mpr != nil || mpu != nil) && len(u.Reach)+len(u.Unreach) > 0:
		return nil, false
	case len(u.Reach) > 0 && len(u.Unreach) == 0:
		return u.Reach, true
	case len(u.Unreach) > 0 && len(u.Reach) == 0:
		return u.Unreach, false
	case mpr != nil && mpu == nil && mpr.AS == afi.AS_IPV6_UNICAST:
		if mp := mpr.Prefixes(); mp != nil {
			return mp.Prefixes, true
		}
	case mpu != nil && mpr == nil && mpu.AS == afi.AS_IPV6_UNICAST:
		if mp := mpu.Prefixes(); mp != nil {
			return mp.Prefixes, false
		}
	}
	return nil, false
}

// peer identifies the BGP peer of a message: its direction, collector and peer tags (if any)
type peer struct {
	collector, router, rib, rd string
	ip                         netip.Addr
	as                         uint32
}

func peerKey(m *msg.Msg) peer {
	t := &m.Tags
	return peer{t.Collector, t.Router, t.Get("bmp/rib"), t.Get("bmp/rd"), t.PeerIP, t.PeerAS}
}

// flush emits the UPDATEs of peers with the first one older than t (all if t is zero)
func (s *Combine) flush(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for peer, cp := range s.peers {
		if t.IsZero() || cp.time.Before(t) {
			s.emit(peer, cp)
		}
	}
}

// emit sends the pending UPDATEs of peer, in order. Must hold s.mu.
func (s *Combine) emit(key peer, cp *combinePeer) {
	if cp == nil {
		return
	}
	delete(s.peers, key)

	for _, g := range cp.groups {
		if u := &g.m.Update; len(g.add) > 0 {
			if u.HasReach() {
				u.AddReach(g.add...)
			} else {
				u.AddUnreach(g.add...)
			}
			g.m.Edit()
		}
		s.in.WriteMsg(g.m)
	}
}
