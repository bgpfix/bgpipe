package stages

import (
	"math"
	"os"
	"sync"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/bgpipe"
)

type Stdout struct {
	*bgpipe.StageBase
	pool sync.Pool
}

func NewStdout(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Stdout{StageBase: parent}

	o := &s.Options
	o.Descr = "print JSON representation to stdout"
	s.Options.IsStdout = true
	o.Bidir = true

	return s
}

func (s *Stdout) Attach() error {
	if s.Index == 0 { // auto stdout
		s.P.AddCallback(s.OnMsg, &pipe.Callback{
			Post:  true,
			Order: math.MaxInt,
			Dir:   s.Dir,
		})
	} else {
		s.P.OnMsg(s.OnMsg, s.Dir)
	}

	return nil
}

func (s *Stdout) OnMsg(m *msg.Msg) (action pipe.Action) {
	// get from pool, marshal
	buf, _ := s.pool.Get().([]byte)
	buf = m.ToJSON(buf[:0])
	buf = append(buf, '\n')

	// write, re-use
	os.Stdout.Write(buf)
	s.pool.Put(buf)

	return
}
