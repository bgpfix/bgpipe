package rpki

import (
	"context"
	"io"
	"net/netip"

	"github.com/VictoriaMetrics/metrics"
	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpfix/rpki"
	"github.com/bgpfix/bgpipe/core"
	"github.com/rs/zerolog"
)

// newTestBase returns a minimal StageBase wired to a fresh pipe.
func newTestBase() *core.StageBase {
	s := &core.StageBase{
		Logger: zerolog.New(io.Discard),
		Ctx:    context.Background(),
	}
	s.P = pipe.NewPipe(context.Background())
	s.P.Options.Logger = nil
	s.P.Options.Caps = false
	s.Dir = dir.DIR_R
	return s
}

// newTestRov returns a Rov stage with an empty RPKI cache, bypassing Attach.
func newTestRov() *Rov {
	s := &Rov{
		StageBase: newTestBase(),
		cache:     rpki.NewCache(nil),
	}
	s.cnt_msg = metrics.GetOrCreateCounter("test_bgpipe_rov_messages_total")
	s.cnt_valid = metrics.GetOrCreateCounter("test_bgpipe_rov_valid_total")
	s.cnt_inv = metrics.GetOrCreateCounter("test_bgpipe_rov_invalid_total")
	s.cnt_nf = metrics.GetOrCreateCounter("test_bgpipe_rov_not_found_total")
	return s
}

// newTestAspa returns an Aspa stage with an empty RPKI cache, bypassing Attach.
func newTestAspa() *Aspa {
	s := &Aspa{
		StageBase: newTestBase(),
		cache:     rpki.NewCache(nil),
		first_hop: true, // matches the --first-hop default
	}
	s.cnt_msg = metrics.GetOrCreateCounter("test_bgpipe_aspa_messages_total")
	s.cnt_valid = metrics.GetOrCreateCounter("test_bgpipe_aspa_valid_total")
	s.cnt_unk = metrics.GetOrCreateCounter("test_bgpipe_aspa_unknown_total")
	s.cnt_inv = metrics.GetOrCreateCounter("test_bgpipe_aspa_invalid_total")
	return s
}

func newReachUpdate(dst dir.Dir, prefixes ...string) *msg.Msg {
	m := msg.NewMsg()
	m.Switch(msg.UPDATE)
	m.Dir = dst
	for _, prefix := range prefixes {
		m.Update.AddReach(nlri.FromPrefix(netip.MustParsePrefix(prefix)))
	}
	return m
}

func setAsPathSeq(m *msg.Msg, asns ...uint32) {
	ap := m.Update.Attrs.Use(attrs.ATTR_ASPATH).(*attrs.Aspath)
	ap.Segments = append(ap.Segments[:0], attrs.AspathSegment{List: asns})
}

func setEmptyAsPath(m *msg.Msg) {
	ap := m.Update.Attrs.Use(attrs.ATTR_ASPATH).(*attrs.Aspath)
	ap.Segments = ap.Segments[:0]
}

func storeOpenASN(p *pipe.Pipe, d dir.Dir, asn int) {
	om := &msg.Open{}
	om.Caps.Init()
	om.SetASN(asn)
	p.LineFor(d).Open.Store(om)
}

func prefixStrings(pp []nlri.Prefix) []string {
	out := make([]string, 0, len(pp))
	for _, prefix := range pp {
		out = append(out, prefix.String())
	}
	return out
}
