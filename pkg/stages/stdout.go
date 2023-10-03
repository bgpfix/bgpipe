package stages

import (
	"math"
	"os"
	"sync"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
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
	o.AllowLR = true

	return s
}

func (s *Stdout) Attach() error {
	po := &s.P.Options

	if s.Index == 0 { // auto stdout
		po.AddCallback(s.OnMsg, &pipe.Callback{
			Post:  true,
			Order: math.MaxInt,
			Dst:   s.Dst(),
		})
	} else {
		po.OnMsg(s.OnMsg, s.Dst())
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
