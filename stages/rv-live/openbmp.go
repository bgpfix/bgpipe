package rvlive

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/bmp"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/twmb/franz-go/pkg/kgo"
)

func (s *RvLive) processRecord(record *kgo.Record) error {
	om, bm := s.obmp_msg, s.bmp_msg

	// Parse OpenBMP header
	om.Reset()
	if _, err := om.FromBytes(record.Value); err != nil {
		return fmt.Errorf("invalid OpenBMP: %w", err)
	}

	// Parse BMP message
	bm.Reset()
	if _, err := bm.FromBytes(om.Data); err != nil {
		return fmt.Errorf("invalid BMP: %w", err)
	}

	// Only process Route Monitoring messages with BGP data
	if bm.MsgType != bmp.MSG_ROUTE_MONITORING || len(bm.BgpData) == 0 {
		return nil
	}

	// Parse BGP message
	P := s.P
	m := P.GetMsg()
	n, err := m.FromBytes(bm.BgpData)
	if err != nil {
		P.PutMsg(m)
		return fmt.Errorf("BGP parse error: %w", err)
	}
	if n != len(bm.BgpData) {
		P.PutMsg(m)
		return fmt.Errorf("dangling bytes after BGP message: %d/%d", n, len(bm.BgpData))
	}

	// Add metadata
	if !bm.Peer.Time.IsZero() {
		m.Time = bm.Peer.Time
	} else if !om.Time.IsZero() {
		m.Time = om.Time
	}
	tags := pipe.UseContext(m).UseTags()
	tags["PEER_IP"] = bm.Peer.Address.String()
	tags["PEER_AS"] = strconv.FormatUint(uint64(bm.Peer.AS), 10)

	// NB: routeviews puts collector in router name field
	if om.RouterName != "" {
		tags["COLLECTOR"] = om.RouterName
	} else if col, _ := parseTopic(record.Topic); col != "" {
		tags["COLLECTOR"] = col
	}

	// set ROUTER to the router IP for consistency with bmp reader
	if om.RouterIP.IsValid() {
		tags["ROUTER"] = om.RouterIP.String()
	}

	// strip collector AS from AS_PATH?
	if !s.keep_aspath {
		s.fixPath(m, bm.Peer.AS)
	}

	// Write to pipe
	m.CopyData()
	return s.in.WriteMsg(m)
}

// fixPath removes the first (collector) ASN from AS_PATH, if peer_as is the second AS in path.
func (s *RvLive) fixPath(m *msg.Msg, peer_as uint32) {
	// need to parse as UPDATE first
	if m.Type != msg.UPDATE || s.P.ParseMsg(m) != nil {
		return
	}

	// get existing AS_PATH, check if sane
	asp := m.Update.AsPath()
	if asp == nil || len(asp.Segments) == 0 {
		return
	}

	// check we should remove the first hop
	seg := &asp.Segments[0]
	if seg.IsSet || len(seg.List) < 2 || seg.List[0] == peer_as || seg.List[1] != peer_as {
		return
	}

	// remove the first ASN from the first segment
	if len(seg.List) > 1 {
		seg.List = seg.List[1:]
	} else if len(asp.Segments) > 1 {
		asp.Segments = asp.Segments[1:]
	} else {
		return // AS_PATH would become empty, leave it as is
	}

	// update the message
	m.Update.Attrs.Set(attrs.ATTR_ASPATH, asp)
	m.Edit()
}

// best-effort parser for topic names: routeviews.<collector-name>.<router-asn>.<other>
var reParseTopic = regexp.MustCompile(`^routeviews\.(.+)\.([0-9]+)\.`)

// parseTopic extracts collector and router/peer ASN from a Kafka topic name.
func parseTopic(topic string) (collector, router string) {
	matches := reParseTopic.FindStringSubmatch(topic)
	if len(matches) == 3 {
		collector = matches[1]
		router = matches[2]
	}
	return collector, router
}
