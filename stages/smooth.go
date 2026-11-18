package stages

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

// Smooth holds announcements for a while, and sends only the last one for each prefix,
// like the BGP MRAI timer. Withdrawals are sent at once. Uses message time of each peer,
// so it works the same on live feeds and on files.
// NB: all messages are sent again through s.in (as copies), to keep their order
type Smooth struct {
	*core.StageBase
	in     *pipe.Input
	window time.Duration

	mu    sync.Mutex
	peers map[string]*smoothPeer
}

// smoothPeer holds the pending announcements of a peer
type smoothPeer struct {
	pfx   map[nlri.Prefix]*smoothPfx // by prefix
	queue []*smoothPfx               // in order of arrival
	clock time.Time                  // the latest message time
	last  time.Time                  // when the last message arrived
}

// smoothPfx is a pending announcement of a prefix
type smoothPfx struct {
	p    nlri.Prefix
	time time.Time // message time of its first announcement
	m    *msg.Msg  // copy of its last announcement
	done bool      // already sent or withdrawn?
}

func NewSmooth(parent *core.StageBase) core.Stage {
	s := &Smooth{StageBase: parent, peers: make(map[string]*smoothPeer)}

	o := &s.Options
	o.Descr = "hold announcements, send only the last one for each prefix"
	o.IsProducer = true
	o.FilterIn = true
	o.Bidir = true

	f := o.Flags
	f.DurationP("window", "w", 5*time.Second, "time to hold an announcement")

	return s
}

func (s *Smooth) Attach() error {
	s.window = s.K.Duration("window")
	if s.window <= 0 {
		return s.Errorf("--window must be positive")
	}
	s.in = s.P.AddInput(s.Dir)
	s.P.OnMsg(s.onMsg, s.Dir)
	return nil
}

func (s *Smooth) Run() error {
	ticker := time.NewTicker(max(s.window/10, 10*time.Millisecond))
	defer ticker.Stop()
	upstream := upstreamDone(s.StageBase)

	for {
		select {
		case <-ticker.C:
			s.flush(false)
		case <-upstream:
			s.flush(true) // all input processed
			return nil
		case <-s.Ctx.Done():
			s.flush(true)
			return context.Cause(s.Ctx)
		}
	}
}

// flush sends the pending announcements of peers idle for the window, or of all peers
func (s *Smooth) flush(all bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, sp := range s.peers {
		if all || time.Since(sp.last) > s.window {
			s.emit(sp, time.Time{})
			delete(s.peers, key)
		}
	}
}

// onMsg holds the announcements in m, or sends a copy of m.
// Always drops m: the copies take its place.
func (s *Smooth) onMsg(m *msg.Msg) bool {
	key := peerKey(m)
	pfx, reach := unicastPrefixes(m)

	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.peers[key]
	if sp == nil {
		sp = &smoothPeer{pfx: make(map[nlri.Prefix]*smoothPfx)}
		s.peers[key] = sp
	}
	sp.last = time.Now()
	if m.Time.After(sp.clock) {
		sp.clock = m.Time
	}

	switch {
	case len(pfx) == 0: // eg. End-of-RIB, KEEPALIVE, or mixed UPDATE: keep the order
		s.emit(sp, time.Time{})
		s.send(s.P.CopyMsg(m))

	case !reach: // withdrawal: cancel pending announcements, send at once
		for _, p := range pfx {
			if pp := sp.pfx[p]; pp != nil {
				pp.done = true
				delete(sp.pfx, p)
			}
		}
		s.send(s.P.CopyMsg(m))

	default: // announcement: hold, since the first one
		c := s.P.CopyMsg(m)
		if c == nil {
			break
		}
		for _, p := range pfx {
			if pp := sp.pfx[p]; pp != nil {
				pp.m = c
			} else {
				pp = &smoothPfx{p: p, time: m.Time, m: c}
				sp.pfx[p] = pp
				sp.queue = append(sp.queue, pp)
			}
		}
	}

	s.emit(sp, sp.clock.Add(-s.window))
	return false
}

// emit sends the pending announcements of sp older than t (all if t is zero),
// in order, packed as in their last UPDATEs. Must hold s.mu.
func (s *Smooth) emit(sp *smoothPeer, t time.Time) {
	var due map[*msg.Msg][]nlri.Prefix
	n := 0
	for _, pp := range sp.queue {
		if !pp.done && !t.IsZero() && !pp.time.Before(t) {
			break
		}
		if !pp.done {
			if due == nil {
				due = make(map[*msg.Msg][]nlri.Prefix)
			}
			due[pp.m] = append(due[pp.m], pp.p)
			delete(sp.pfx, pp.p)
		}
		n++
	}
	sp.queue = sp.queue[n:]

	ms := slices.Collect(maps.Keys(due))
	slices.SortFunc(ms, func(a, b *msg.Msg) int { return int(a.Seq - b.Seq) })
	for _, m := range ms {
		s.send(s.restrict(m, due[m]))
	}
}

// restrict returns a copy of UPDATE m, announcing only pfx
func (s *Smooth) restrict(m *msg.Msg, pfx []nlri.Prefix) *msg.Msg {
	c := s.P.CopyMsg(m)
	if c == nil || c.Parse(s.P.Caps) != nil {
		return c
	}
	if all, _ := unicastPrefixes(c); len(all) == len(pfx) {
		return c // all still pending
	}
	keep := make(map[nlri.Prefix]bool, len(pfx))
	for _, p := range pfx {
		keep[p] = true
	}
	drop := func(p nlri.Prefix) bool { return !keep[p] }
	u := &c.Update
	u.Reach = slices.DeleteFunc(u.Reach, drop)
	if mp := u.ReachMP().Prefixes(); mp != nil {
		mp.Prefixes = slices.DeleteFunc(mp.Prefixes, drop)
	}
	c.Edit()
	return c
}

// send writes m to s.in, if not nil
func (s *Smooth) send(m *msg.Msg) {
	if m != nil {
		s.in.WriteMsg(m)
	}
}
