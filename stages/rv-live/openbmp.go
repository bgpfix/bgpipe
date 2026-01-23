package rvlive

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bgpfix/bgpfix/bmp"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/twmb/franz-go/pkg/kgo"
)

func (s *RvLive) processRecord(record *kgo.Record) error {
	// Extract collector and router from topic name
	// Topic format: routeviews.<collector>.<peer_asn>.bmp_raw
	collector, router := parseTopicName(record.Topic)

	// Parse OpenBMP header
	bmpData, err := s.parseOpenBmpHeader(record.Value)
	if err != nil {
		return fmt.Errorf("invalid OpenBMP header: %w", err)
	}

	// Trace log parsed OpenBMP header and BMP data
	if e := s.Trace(); e.Enabled() {
		e.Str("collector", collector).
			Str("router", router).
			Int("bmp_len", len(bmpData)).
			Hex("bmp_data", bmpData).
			Msg("parsed OpenBMP message")
	}

	// Parse BMP message
	s.bmpMsg.Reset()
	_, err = s.bmpMsg.FromBytes(bmpData)
	if err != nil {
		return fmt.Errorf("invalid BMP message: %w", err)
	}

	// Only process Route Monitoring messages
	if s.bmpMsg.MsgType != bmp.MSG_ROUTE_MONITORING {
		s.Debug().Str("type", s.bmpMsg.MsgType.String()).Msg("skipping non-Route-Monitoring BMP message")
		return nil
	}

	// Skip if no BGP data
	if len(s.bmpMsg.BgpData) == 0 {
		s.Debug().Msg("skipping Route Monitoring message with no BGP data")
		return nil
	}

	// Trace log extracted BGP data
	if e := s.Trace(); e.Enabled() {
		e.Int("bgp_len", len(s.bmpMsg.BgpData)).
			Hex("bgp_data", s.bmpMsg.BgpData).
			Str("peer_ip", s.bmpMsg.Peer.Address.String()).
			Uint32("peer_as", s.bmpMsg.Peer.AS).
			Msg("extracted BGP message from BMP")
	}

	// Parse BGP message
	P := s.P
	msg := P.GetMsg()
	n, err := msg.FromBytes(s.bmpMsg.BgpData)
	if err != nil {
		P.PutMsg(msg)
		return fmt.Errorf("BGP parse error: %w", err)
	}
	if n != len(s.bmpMsg.BgpData) {
		P.PutMsg(msg)
		return fmt.Errorf("dangling bytes after BGP message: %d/%d", n, len(s.bmpMsg.BgpData))
	}

	// Add metadata
	msg.Time = s.bmpMsg.Peer.Time
	tags := pipe.UseContext(msg).UseTags()
	tags["PEER_IP"] = s.bmpMsg.Peer.Address.String()
	tags["PEER_AS"] = strconv.FormatUint(uint64(s.bmpMsg.Peer.AS), 10)
	tags["RV_COLLECTOR"] = collector
	tags["RV_ROUTER"] = router

	// Write to pipe
	msg.CopyData()
	return s.in.WriteMsg(msg)
}

// parseTopicName extracts collector and router/peer ASN from a Kafka topic name.
// Topic format: routeviews.<collector>.<peer_asn>.bmp_raw
func parseTopicName(topic string) (collector, router string) {
	parts := strings.Split(topic, ".")
	if len(parts) >= 4 {
		collector = parts[1] // e.g., "route-views7"
		router = parts[2]    // e.g., "48112" (peer ASN)
	}
	return
}

// parseOpenBmpHeader parses the OpenBMP binary header (used in bmp_raw topics).
// Format (binary):
//
//	OBMP (4 bytes): Magic
//	Version (1 byte): 0x01
//	Type (1 byte): 0x07 for BMP_RAW
//	Header Length (2 bytes): length of the OpenBMP header (big endian)
//	BMP Message Length (4 bytes): length of the BMP message that follows (big endian)
//	Flags (2 bytes)
//	Collector Hash (16 bytes): MD5 hash
//	... other header fields ...
//	[at offset Header Length]: BMP Message (BMP Message Length bytes)
func (s *RvLive) parseOpenBmpHeader(data []byte) (bmpData []byte, err error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("message too short: %d bytes", len(data))
	}

	// Check magic "OBMP"
	if string(data[0:4]) != "OBMP" {
		return nil, fmt.Errorf("invalid magic: expected OBMP, got %x", data[0:4])
	}

	version := data[4]
	msgType := data[5]
	headerLen := uint16(data[6])<<8 | uint16(data[7])
	bmpLen := uint32(data[8])<<24 | uint32(data[9])<<16 | uint32(data[10])<<8 | uint32(data[11])

	s.Trace().
		Uint8("version", version).
		Uint8("type", msgType).
		Uint16("header_len", headerLen).
		Uint32("bmp_len", bmpLen).
		Msg("parsed OpenBMP header")

	if version != 0x01 {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}

	if msgType != 0x07 {
		return nil, fmt.Errorf("unexpected message type: %d (expected 7 for BMP_RAW)", msgType)
	}

	// Validate lengths
	if int(headerLen) > len(data) {
		return nil, fmt.Errorf("header length %d exceeds buffer %d", headerLen, len(data))
	}

	expectedTotal := int(headerLen) + int(bmpLen)
	if expectedTotal > len(data) {
		return nil, fmt.Errorf("total length %d (header %d + bmp %d) exceeds buffer %d",
			expectedTotal, headerLen, bmpLen, len(data))
	}

	// BMP data starts at headerLen offset
	bmpData = data[headerLen : headerLen+uint16(bmpLen)]

	s.Trace().
		Int("bmp_offset", int(headerLen)).
		Int("bmp_len", len(bmpData)).
		Hex("bmp_start", bmpData[:min(16, len(bmpData))]).
		Msg("extracted BMP data")

	return bmpData, nil
}
