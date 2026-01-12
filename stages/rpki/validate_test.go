package rpki

import (
	"net/netip"
	"testing"
)

func TestValidatePrefixExactMatch(t *testing.T) {
	s := &Rpki{}

	// Setup ROA: 192.0.2.0/24-24 AS65001
	roa4 := make(ROA)
	roa4[netip.MustParsePrefix("192.0.2.0/24")] = []ROAEntry{
		{MaxLen: 24, ASN: 65001},
	}

	tests := []struct {
		name   string
		prefix string
		origin uint32
		want   int
	}{
		{"exact match valid", "192.0.2.0/24", 65001, rpki_valid},
		{"exact match wrong ASN", "192.0.2.0/24", 65002, rpki_invalid},
		{"no ROA", "203.0.113.0/24", 65001, rpki_not_found},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := netip.MustParsePrefix(tt.prefix)
			got := s.validatePrefix(roa4, nil, p, tt.origin)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidatePrefixMaxLen(t *testing.T) {
	s := &Rpki{}

	// Setup ROA: 192.0.2.0/24-26 AS65001 (allows up to /26)
	roa4 := make(ROA)
	roa4[netip.MustParsePrefix("192.0.2.0/24")] = []ROAEntry{
		{MaxLen: 26, ASN: 65001},
	}

	tests := []struct {
		name   string
		prefix string
		origin uint32
		want   int
	}{
		{"within maxLen /24", "192.0.2.0/24", 65001, rpki_valid},
		{"within maxLen /25", "192.0.2.0/25", 65001, rpki_valid},
		{"within maxLen /26", "192.0.2.0/26", 65001, rpki_valid},
		{"exceeds maxLen /27", "192.0.2.0/27", 65001, rpki_invalid},
		{"exceeds maxLen /28", "192.0.2.0/28", 65001, rpki_invalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := netip.MustParsePrefix(tt.prefix)
			got := s.validatePrefix(roa4, nil, p, tt.origin)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidatePrefixCoveringROA(t *testing.T) {
	s := &Rpki{}
	s.nextFlush() // Initialize next4/next6 maps

	// Setup ROA: 192.0.2.0/22-24 AS65001 (covers /22, /23, /24)
	roa4 := make(ROA)
	// Must use .Masked() to match how ROAs are stored in nextAdd()
	roa4[netip.MustParsePrefix("192.0.2.0/22").Masked()] = []ROAEntry{
		{MaxLen: 24, ASN: 65001},
	}

	tests := []struct {
		name   string
		prefix string
		origin uint32
		want   int
	}{
		{"covered /22 valid", "192.0.2.0/22", 65001, rpki_valid},
		{"covered /23 valid", "192.0.2.0/23", 65001, rpki_valid},
		{"covered /24 valid", "192.0.2.0/24", 65001, rpki_valid},
		{"covered /24 different subnet valid", "192.0.3.0/24", 65001, rpki_valid},
		{"exceeds maxLen /25", "192.0.2.0/25", 65001, rpki_invalid},
		{"covered wrong ASN", "192.0.2.0/23", 65002, rpki_invalid},
		{"outside range", "192.0.6.0/24", 65001, rpki_not_found},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := netip.MustParsePrefix(tt.prefix)
			got := s.validatePrefix(roa4, nil, p, tt.origin)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidatePrefixIPv6(t *testing.T) {
	s := &Rpki{}

	// Setup ROA: 2001:db8::/32-48 AS65001
	roa6 := make(ROA)
	roa6[netip.MustParsePrefix("2001:db8::/32")] = []ROAEntry{
		{MaxLen: 48, ASN: 65001},
	}

	tests := []struct {
		name   string
		prefix string
		origin uint32
		want   int
	}{
		{"exact match", "2001:db8::/32", 65001, rpki_valid},
		{"covered /48", "2001:db8:1234::/48", 65001, rpki_valid},
		{"exceeds maxLen /64", "2001:db8:1234:5678::/64", 65001, rpki_invalid},
		{"wrong ASN", "2001:db8::/32", 65002, rpki_invalid},
		{"different prefix", "2001:db9::/32", 65001, rpki_not_found},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := netip.MustParsePrefix(tt.prefix)
			got := s.validatePrefix(nil, roa6, p, tt.origin)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidatePrefixMultipleROAs(t *testing.T) {
	s := &Rpki{}

	// Setup: Multiple ROAs for same prefix (MOAS scenario)
	roa4 := make(ROA)
	roa4[netip.MustParsePrefix("192.0.2.0/24")] = []ROAEntry{
		{MaxLen: 24, ASN: 65001},
		{MaxLen: 26, ASN: 65002},
		{MaxLen: 24, ASN: 65003},
	}

	tests := []struct {
		name   string
		prefix string
		origin uint32
		want   int
	}{
		{"match AS65001", "192.0.2.0/24", 65001, rpki_valid},
		{"match AS65002 /24", "192.0.2.0/24", 65002, rpki_valid},
		{"match AS65002 /26", "192.0.2.0/26", 65002, rpki_valid},
		{"AS65001 exceeds maxLen", "192.0.2.0/25", 65001, rpki_invalid},
		{"AS65003 /24", "192.0.2.0/24", 65003, rpki_valid},
		{"no matching ASN", "192.0.2.0/24", 65999, rpki_invalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := netip.MustParsePrefix(tt.prefix)
			got := s.validatePrefix(roa4, nil, p, tt.origin)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidatePrefixStrictMode(t *testing.T) {
	s := &Rpki{strict: true}

	// Empty ROA cache
	roa4 := make(ROA)

	p := netip.MustParsePrefix("192.0.2.0/24")
	got := s.validatePrefix(roa4, nil, p, 65001)

	// In strict mode, NOT_FOUND should return INVALID
	if got != rpki_invalid {
		t.Errorf("strict mode: got %d, want rpki_invalid", got)
	}
}

func TestValidatePrefixMinROALen(t *testing.T) {
	s := &Rpki{}

	// ROA for /7 (too short, below minROALenV4)
	roa4 := make(ROA)
	roa4[netip.MustParsePrefix("128.0.0.0/7")] = []ROAEntry{
		{MaxLen: 24, ASN: 65001},
	}

	// /24 within /7 range - should NOT match (stops at /8)
	p := netip.MustParsePrefix("128.1.0.0/24")
	got := s.validatePrefix(roa4, nil, p, 65001)

	if got != rpki_not_found {
		t.Errorf("should not check beyond minROALenV4, got %d", got)
	}
}

func TestValidatePrefixEmptyCache(t *testing.T) {
	s := &Rpki{}

	// Nil and empty caches
	tests := []struct {
		name string
		roa4 ROA
		roa6 ROA
	}{
		{"nil cache", nil, nil},
		{"empty map v4", ROA{}, nil},
		{"empty map v6", nil, ROA{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p4 := netip.MustParsePrefix("192.0.2.0/24")
			p6 := netip.MustParsePrefix("2001:db8::/32")

			got4 := s.validatePrefix(tt.roa4, nil, p4, 65001)
			got6 := s.validatePrefix(nil, tt.roa6, p6, 65001)

			if got4 != rpki_not_found || got6 != rpki_not_found {
				t.Errorf("empty cache should return NOT_FOUND")
			}
		})
	}
}
