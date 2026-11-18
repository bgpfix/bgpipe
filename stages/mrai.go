package stages

import (
	"slices"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

// Mrai holds announcements and sends the last one of each prefix at most once per
// window and peer, like the BGP MRAI timer. Withdrawals are sent at once.
// NB: uses message time of each peer, so works the same on live feeds and files
type Mrai struct {
	*core.StageBase
	in     *pipe.Input
	window time.Duration

	mu    sync.Mutex
	peers map[peer]*mraiPeer
}

// mraiPeer holds the pending announcements of a peer
type mraiPeer struct {
	msgs  []*msg.Msg               // taken UPDATEs, in order of arrival
	last  map[nlri.Prefix]*msg.Msg // the last announcement of each prefix
	since time.Time                // message time of the first one
	clock time.Time                // the latest message time
	seen  time.Time                // when the latest message arrived
}

func NewMrai(parent *core.StageBase) core.Stage {
	s := &Mrai{StageBase: parent, peers: make(map[peer]*mraiPeer)}

	o := &s.Options
	o.Descr = "send prefix announcements at most once per time window (MRAI)"
	o.IsProducer = true
	o.FilterIn = true
	o.Bidir = true
	o.StopUpstream = true

	f := o.Flags
	f.DurationP("window", "w", 5*time.Second, "min. time between announcements of a prefix")

	return s
}

func (s *Mrai) Attach() error {
	s.window = s.K.Duration("window")
	if s.window <= 0 {
		return s.Errorf("--window must be positive")
	}
	s.in = s.P.AddInput(s.Dir)
	s.P.OnMsg(s.onMsg, s.Dir)
	return nil
}

func (s *Mrai) Run() error {
	ticker := time.NewTicker(max(s.window/10, 10*time.Millisecond))
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.flush(false)
		case <-s.Ctx.Done():
			s.flush(true)
			return nil
		}
	}
}

// flush sends the pending announcements of idle peers, or of all peers
func (s *Mrai) flush(all bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, sp := range s.peers {
		if all || time.Since(sp.seen) > s.window {
			s.emit(sp)
			delete(s.peers, key)
		}
	}
}

// onMsg holds the announcements in m, or sends m right away
func (s *Mrai) onMsg(m *msg.Msg) bool {
	key := peerKey(m)
	pfx, reach := unicastPrefixes(m)

	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.peers[key]
	if sp == nil {
		sp = &mraiPeer{last: make(map[nlri.Prefix]*msg.Msg)}
		s.peers[key] = sp
	}
	sp.seen = time.Now()
	if m.Time.After(sp.clock) {
		sp.clock = m.Time
	}

	pipe.UseContext(m).Take()
	switch {
	case len(pfx) == 0: // eg. End-of-RIB, KEEPALIVE, mixed UPDATE: keep the order
		s.emit(sp)
		s.in.WriteMsg(m)
	case !reach: // withdrawal: cancel pending announcements
		for _, p := range pfx {
			delete(sp.last, p)
		}
		s.in.WriteMsg(m)
	default: // announcement: hold
		if len(sp.msgs) == 0 {
			sp.since = m.Time
		}
		sp.msgs = append(sp.msgs, m)
		for _, p := range pfx {
			sp.last[p] = m
		}
	}

	if len(sp.msgs) > 0 && !sp.clock.Before(sp.since.Add(s.window)) {
		s.emit(sp)
	}
	return false
}

// emit sends the pending announcements of sp: each held UPDATE, with only the
// prefixes it was the last announcement of. Must hold s.mu.
func (s *Mrai) emit(sp *mraiPeer) {
	for _, m := range sp.msgs {
		stale := func(p nlri.Prefix) bool { return sp.last[p] != m }
		u := &m.Update
		pfx, _ := unicastPrefixes(m)
		n := len(pfx)
		if slices.ContainsFunc(pfx, stale) {
			u.Reach = slices.DeleteFunc(u.Reach, stale)
			if mp := u.ReachMP().Prefixes(); mp != nil {
				mp.Prefixes = slices.DeleteFunc(mp.Prefixes, stale)
				n = len(mp.Prefixes)
			} else {
				n = len(u.Reach)
			}
			m.Edit()
		}
		if n > 0 {
			s.in.WriteMsg(m)
		} else {
			s.P.PutMsg(m) // all superseded or withdrawn
		}
	}
	sp.msgs = sp.msgs[:0]
	clear(sp.last)
}
