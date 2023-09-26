package stages

import (
	"net/netip"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/speaker"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Speaker struct {
	*bgpipe.StageBase

	spk *speaker.Speaker
}

func NewSpeaker(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Speaker{StageBase: parent}
	s.Descr = "run a simple local BGP speaker"
	s.Flags.Bool("active", false, "send the OPEN message first")
	s.Flags.Int("asn", -1, "local ASN, -1 means use remote ASN")
	s.Flags.String("id", "", "local router ID, empty means use remote-1")
	s.IsWriter = true
	return s
}

func (s *Speaker) Prepare() error {
	k := s.K

	spk := speaker.NewSpeaker(s.Ctx)
	s.spk = spk

	so := &spk.Options
	so.Writer = s.send
	so.Logger = s.Logger
	so.Passive = !k.Bool("active")
	so.LocalASN = k.Int("asn")

	lid := k.String("id")
	if len(lid) > 0 {
		so.LocalId = netip.MustParseAddr(lid)
	} else if so.Passive {
		so.LocalId = netip.Addr{}
	} else {
		so.LocalId = netip.MustParseAddr("0.0.0.1")
	}

	return spk.Attach(s.Upstream())
}

// TODO: export to base
func (s *Speaker) send(m *msg.Msg) error {
	// pc := pipe.PipeContext(m)
	// pc.SkipAfter(nil)
	s.Printf("TODO: speaker/send")
	return s.Upstream().WriteMsg(m)
}
