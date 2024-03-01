package stages

import (
	"math"
	"os"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	bgpipe "github.com/bgpfix/bgpipe/core"
)

type Stdout struct {
	*bgpipe.StageBase
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
		})
	} else {
		s.P.OnMsg(s.OnMsg, s.Dir)
	}

	return nil
}

func (s *Stdout) OnMsg(m *msg.Msg) {
	os.Stdout.Write(m.GetJSON())
	os.Stdout.WriteString("\n")
}
