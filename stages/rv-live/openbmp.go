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
	om, bm := s.obmpMsg, s.bmpMsg

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

	// Add metadata from BMP peer header
	m.Time = bm.Peer.Time
	tags := pipe.UseContext(m).UseTags()
	tags["PEER_IP"] = bm.Peer.Address.String()
	tags["PEER_AS"] = strconv.FormatUint(uint64(bm.Peer.AS), 10)

	// Add metadata from OpenBMP header (prefer over topic name parsing)
	if om.CollectorName != "" {
		tags["RV_COLLECTOR"] = om.CollectorName
	}
	if om.RouterName != "" {
		tags["RV_ROUTER"] = om.RouterName
	} else if om.RouterIP.IsValid() {
		tags["RV_ROUTER"] = om.RouterIP.String()
	}

	// Fallback to topic name parsing if metadata not in header
	if tags["RV_COLLECTOR"] == "" || tags["RV_ROUTER"] == "" {
		collector, router := parseTopic(record.Topic)
		if tags["RV_COLLECTOR"] == "" {
			tags["RV_COLLECTOR"] = collector
		}
		if tags["RV_ROUTER"] == "" {
			tags["RV_ROUTER"] = router
		}
	}

	// Strip RouteViews collector ASN 6447 from AS_PATH unless --keep-aspath
	if !s.keepAspath {
		s.stripCollectorAsn(m)
	}

	// Write to pipe
	m.CopyData()
	return s.in.WriteMsg(m)
}

// stripCollectorAsn removes the RouteViews collector ASN 6447 from the first hop of AS_PATH
func (s *RvLive) stripCollectorAsn(m *msg.Msg) {
	const RV_ASN = 6447

	// need to parse as UPDATE first
	if m.Type != msg.UPDATE || s.P.ParseMsg(m) != nil {
		return
	}

	// get existing AS_PATH, check if sane
	asp := m.Update.AsPath()
	if asp == nil || len(asp.Segments) == 0 {
		return
	}

	// check if the first hop is the collector ASN
	seg := &asp.Segments[0]
	if seg.IsSet || len(seg.List) == 0 || seg.List[0] != RV_ASN {
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

var reParseTopic = regexp.MustCompile(`^routeviews\.(.+)\.([0-9]+)\.bmp_raw$`)

// parseTopic extracts collector and router/peer ASN from a Kafka topic name.
func parseTopic(topic string) (collector, router string) {
	matches := reParseTopic.FindStringSubmatch(topic)
	if len(matches) == 3 {
		collector = matches[1]
		router = matches[2]
	}
	return collector, router
}
