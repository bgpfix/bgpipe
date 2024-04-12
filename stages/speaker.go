package stages

import (
	"net/netip"

	"github.com/bgpfix/bgpfix/speaker"
	"github.com/bgpfix/bgpipe/core"
)

type Speaker struct {
	*core.StageBase

	spk *speaker.Speaker
}

func NewSpeaker(parent *core.StageBase) core.Stage {
	s := &Speaker{StageBase: parent}

	o := &s.Options
	o.Descr = "run a simple local BGP speaker"
	o.IsProducer = true

	do := &speaker.DefaultOptions
	o.Flags.Bool("active", false, "send the OPEN message first")
	o.Flags.Int("asn", do.LocalASN, "local ASN, -1 means use remote ASN")
	o.Flags.String("id", "", "local router ID, empty means use remote-1")
	o.Flags.Int("hold", do.LocalHoldTime, "hold time")
	return s
}

func (s *Speaker) Attach() error {
	k := s.K

	spk := speaker.NewSpeaker(s.Ctx)
	s.spk = spk

	so := &spk.Options
	so.Logger = &s.Logger
	so.Passive = !k.Bool("active")
	so.LocalASN = k.Int("asn")
	so.LocalHoldTime = k.Int("hold")

	lid := k.String("id")
	if len(lid) > 0 {
		so.LocalId = netip.MustParseAddr(lid)
	} else if so.Passive {
		so.LocalId = netip.Addr{}
	} else {
		so.LocalId = netip.MustParseAddr("0.0.0.1")
	}

	return spk.Attach(s.P, s.Dir)
}
