package stages

import (
	"sync/atomic"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpipe/core"
)

type Head struct {
	*core.StageBase

	limit int64        // max number of messages to pass
	count atomic.Int64 // number of messages seen so far
}

func NewHead(parent *core.StageBase) core.Stage {
	s := &Head{StageBase: parent}

	o := &s.Options
	o.Descr = "stop pipeline after N messages"
	o.Bidir = true
	o.FilterIn = true

	f := o.Flags
	f.Int64P("count", "n", 10, "number of messages to pass before stopping")

	return s
}

func (s *Head) Attach() error {
	k := s.K

	s.limit = k.Int64("count")
	if s.limit <= 0 {
		return s.Errorf("count must be positive")
	}

	// register callback
	s.P.OnMsg(s.onMsg, s.Dir)
	return nil
}

func (s *Head) onMsg(m *msg.Msg) bool {
	// increment counter and check limit
	n := s.count.Add(1)

	if n < s.limit {
		// under limit, pass message
		return true
	} else if n == s.limit {
		// at limit, pass message and stop pipe
		go s.P.Stop()
		return true
	} else {
		// already over limit, drop message
		return false
	}
}
