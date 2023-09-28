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
	s.Options.Descr = "print JSON representation to stdout"

	// TODO: modify --left/--right default options?

	// f := s.Options.Flags
	// f.StringSlice("grep", []string{}, "print only given types")
	// f.StringSlice("filter", []string{}, "filter given types")

	s.Options.IsStdout = true
	return s
}

func (s *Stdout) Attach() error {
	// TODO: grep /filter
	// for _, t := range s.K.Strings("grep") {
	// }

	// by default, set LR
	// FIXME: in parent
	if !(s.K.Bool("left") || s.K.Bool("right")) {
		s.IsLeft = true
		s.IsRight = true
	}

	po := &s.P.Options
	if s.K.Bool("auto") {
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
