package stages

import (
	"net/netip"

	"github.com/bgpfix/bgpfix/speaker"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Speaker struct {
	*bgpipe.StageBase

	spk *speaker.Speaker
}

func NewSpeaker(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Speaker{StageBase: parent}
	s.Options.Descr = "run a simple local BGP speaker"
	s.Options.Flags.Bool("active", false, "send the OPEN message first")
	s.Options.Flags.Int("asn", -1, "local ASN, -1 means use remote ASN")
	s.Options.Flags.String("id", "", "local router ID, empty means use remote-1")
	s.Options.IsProducer = true
	return s
}

func (s *Speaker) Attach() error {
	k := s.K

	spk := speaker.NewSpeaker(s.Ctx)
	s.spk = spk

	so := &spk.Options
	so.Logger = &s.Logger
	so.NewMsg = s.NewMsg
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
