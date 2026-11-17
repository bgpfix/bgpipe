package stages

import (
	"context"
	"maps"
	"slices"
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
// NB: all messages are sent again through s.in (as copies), to keep their order
type Combine struct {
	*core.StageBase
	in *pipe.Input

	window  time.Duration // max. time to hold an UPDATE
	maxsize int           // max. size of a combined UPDATE

	mu    sync.Mutex
	peers map[string]*combinePeer // pending UPDATEs, by peer
}

// combinePeer holds the pending UPDATEs of one peer
type combinePeer struct {
	time   time.Time                // when the first UPDATE arrived
	groups []*combineGroup          // in order of arrival
	bykey  map[string]*combineGroup // by attributes
	pfx    map[nlri.Prefix]bool     // all pending prefixes
}

// combineGroup is one combined UPDATE: a copy of the first one, plus prefixes of the next ones
type combineGroup struct {
	m    *msg.Msg
	add  []nlri.Prefix // prefixes to add to m
	size int           // estimated size of m
}

func NewCombine(parent *core.StageBase) core.Stage {
	s := &Combine{StageBase: parent, peers: make(map[string]*combinePeer)}

	o := &s.Options
	o.Descr = "combine UPDATEs with the same attributes into one"
	o.IsProducer = true
	o.FilterIn = true
	o.Bidir = true

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
	upstream := s.upstream()

	for {
		select {
		case now := <-ticker.C:
			s.flush(now.Add(-s.window))
		case <-upstream:
			s.flush(time.Time{}) // all input processed
			return nil
		case <-s.Ctx.Done():
			s.flush(time.Time{})
			return context.Cause(s.Ctx)
		}
	}
}

// upstream returns a channel closed when all producers before s are done.
// NB: a stage is done after its input is drained, so s saw all its messages
func (s *Combine) upstream() <-chan struct{} {
	var ctxs []context.Context
	for _, st := range append(slices.Clone(s.B.Stages), s.B.Autos...) {
		if st == nil || st == s.StageBase || !st.Options.IsProducer {
			continue
		}
		if (s.IsRight && st.IsRight && st.Index < s.Index) || (s.IsLeft && st.IsLeft && st.Index > s.Index) {
			ctxs = append(ctxs, st.Ctx)
		}
	}

	done := make(chan struct{})
	go func() {
		for _, ctx := range ctxs {
			select {
			case <-ctx.Done():
			case <-s.Ctx.Done():
				return
			}
		}
		close(done)
	}()
	return done
}

// onMsg takes m into a pending UPDATE, or sends its copy right after the pending ones.
// Always drops m: the copies take its place.
func (s *Combine) onMsg(m *msg.Msg) bool {
	peer := combinePeerKey(m)
	key, pfx := s.classify(m)

	s.mu.Lock()
	defer s.mu.Unlock()

	// can't combine m? (eg. End-of-RIB, KEEPALIVE)
	cp := s.peers[peer]
	if key == "" {
		s.emit(peer, cp)
		s.send(s.P.CopyMsg(m))
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

	// add to the pending UPDATE, if it fits
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

	// start a new pending UPDATE with a copy of m
	c := s.P.CopyMsg(m)
	if c == nil {
		return false // can't marshal m
	}
	g := &combineGroup{m: c, size: c.Len()}
	cp.groups = append(cp.groups, g)
	cp.bykey[key] = g
	return false
}

// classify returns the attributes key and prefixes of m, or an empty key if m can't be combined.
// Only IPv4 and IPv6 unicast UPDATEs that only announce, or only withdraw, can be combined.
func (s *Combine) classify(m *msg.Msg) (key string, pfx []nlri.Prefix) {
	if m.Type != msg.UPDATE {
		return "", nil
	}
	u := &m.Update
	mpr, mpu := u.ReachMP(), u.UnreachMP()
	v4, v6 := len(u.Reach)+len(u.Unreach) > 0, mpr != nil || mpu != nil

	var buf []byte
	switch {
	case v4 && v6:
		return "", nil
	case len(u.Reach) > 0 && len(u.Unreach) == 0:
		buf, pfx = append(buf, 'A', '4'), u.Reach
	case len(u.Unreach) > 0 && len(u.Reach) == 0:
		return "W4", u.Unreach
	case mpr != nil && mpu == nil && mpr.AS == afi.AS_IPV6_UNICAST:
		mp := mpr.Prefixes()
		if mp == nil {
			return "", nil
		}
		buf, pfx = append(buf, 'A', '6'), mp.Prefixes
		buf = append(buf, mp.NextHop.AsSlice()...)
		buf = append(buf, mp.LinkLocal.AsSlice()...)
	case mpu != nil && mpr == nil && mpu.AS == afi.AS_IPV6_UNICAST:
		if mp := mpu.Prefixes(); mp != nil {
			return "W6", mp.Prefixes
		}
		return "", nil
	default:
		return "", nil
	}
	if len(pfx) == 0 {
		return "", nil
	}

	// the attributes, without MP_REACH (its next-hop is in buf already)
	u.Attrs.Each(func(_ int, ac attrs.Code, at attrs.Attr) {
		if ac != attrs.ATTR_MP_REACH {
			buf = at.Marshal(buf, s.P.Caps, m.Meta)
		}
	})
	return string(buf), pfx
}

// combinePeerKey identifies the peer of m by its direction and tags
func combinePeerKey(m *msg.Msg) string {
	tags := pipe.GetTags(m)
	buf := []byte{byte(m.Dir)}
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		buf = append(buf, k...)
		buf = append(buf, '=')
		buf = append(buf, tags[k]...)
		buf = append(buf, '\n')
	}
	return string(buf)
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
func (s *Combine) emit(peer string, cp *combinePeer) {
	if cp == nil {
		return
	}
	delete(s.peers, peer)

	for _, g := range cp.groups {
		m, u := g.m, &g.m.Update
		if len(g.add) > 0 && m.Parse(s.P.Caps) == nil {
			if u.HasReach() {
				u.AddReach(g.add...)
			} else {
				u.AddUnreach(g.add...)
			}
			m.Edit()
		}
		s.send(m)
	}
}

// send writes m to s.in, if not nil
func (s *Combine) send(m *msg.Msg) {
	if m != nil {
		s.in.WriteMsg(m)
	}
}
