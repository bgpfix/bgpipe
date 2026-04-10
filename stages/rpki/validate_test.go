package rpki

import (
	"net/netip"
	"testing"
)

func TestValidatePrefixExactMatch(t *testing.T) {
	s := &Rpki{}

	// VRP: 192.0.2.0/24-24 AS65001
	roa4 := make(VRPs)
	roa4[netip.MustParsePrefix("192.0.2.0/24")] = []VRP{
		{MaxLen: 24, ASN: 65001},
	}

	tests := []struct {
		name   string
		prefix string
		origin uint32
		want   int
	}{
		{"exact match valid", "192.0.2.0/24", 65001, rov_valid},
		{"exact match wrong ASN", "192.0.2.0/24", 65002, rov_invalid},
		{"no VRP", "203.0.113.0/24", 65001, rov_not_found},
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

	// VRP: 192.0.2.0/24-26 AS65001 (allows up to /26)
	roa4 := make(VRPs)
	roa4[netip.MustParsePrefix("192.0.2.0/24")] = []VRP{
		{MaxLen: 26, ASN: 65001},
	}

	tests := []struct {
		name   string
		prefix string
		origin uint32
		want   int
	}{
		{"within maxLen /24", "192.0.2.0/24", 65001, rov_valid},
		{"within maxLen /25", "192.0.2.0/25", 65001, rov_valid},
		{"within maxLen /26", "192.0.2.0/26", 65001, rov_valid},
		{"exceeds maxLen /27", "192.0.2.0/27", 65001, rov_invalid},
		{"exceeds maxLen /28", "192.0.2.0/28", 65001, rov_invalid},
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

	// VRP: 192.0.2.0/22-24 AS65001 (covers /22, /23, /24)
	roa4 := make(VRPs)
	// Must use .Masked() to match how ROAs are stored in nextAdd()
	roa4[netip.MustParsePrefix("192.0.2.0/22").Masked()] = []VRP{
		{MaxLen: 24, ASN: 65001},
	}

	tests := []struct {
		name   string
		prefix string
		origin uint32
		want   int
	}{
		{"covered /22 valid", "192.0.2.0/22", 65001, rov_valid},
		{"covered /23 valid", "192.0.2.0/23", 65001, rov_valid},
		{"covered /24 valid", "192.0.2.0/24", 65001, rov_valid},
		{"covered /24 different subnet valid", "192.0.3.0/24", 65001, rov_valid},
		{"exceeds maxLen /25", "192.0.2.0/25", 65001, rov_invalid},
		{"covered wrong ASN", "192.0.2.0/23", 65002, rov_invalid},
		{"outside range", "192.0.6.0/24", 65001, rov_not_found},
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

	// VRP: 2001:db8::/32-48 AS65001
	roa6 := make(VRPs)
	roa6[netip.MustParsePrefix("2001:db8::/32")] = []VRP{
		{MaxLen: 48, ASN: 65001},
	}

	tests := []struct {
		name   string
		prefix string
		origin uint32
		want   int
	}{
		{"exact match", "2001:db8::/32", 65001, rov_valid},
		{"covered /48", "2001:db8:1234::/48", 65001, rov_valid},
		{"exceeds maxLen /64", "2001:db8:1234:5678::/64", 65001, rov_invalid},
		{"wrong ASN", "2001:db8::/32", 65002, rov_invalid},
		{"different prefix", "2001:db9::/32", 65001, rov_not_found},
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
	roa4 := make(VRPs)
	roa4[netip.MustParsePrefix("192.0.2.0/24")] = []VRP{
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
		{"match AS65001", "192.0.2.0/24", 65001, rov_valid},
		{"match AS65002 /24", "192.0.2.0/24", 65002, rov_valid},
		{"match AS65002 /26", "192.0.2.0/26", 65002, rov_valid},
		{"AS65001 exceeds maxLen", "192.0.2.0/25", 65001, rov_invalid},
		{"AS65003 /24", "192.0.2.0/24", 65003, rov_valid},
		{"no matching ASN", "192.0.2.0/24", 65999, rov_invalid},
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

	// empty VRP cache
	roa4 := make(VRPs)

	p := netip.MustParsePrefix("192.0.2.0/24")
	got := s.validatePrefix(roa4, nil, p, 65001)

	// In strict mode, NOT_FOUND should return INVALID
	if got != rov_invalid {
		t.Errorf("strict mode: got %d, want rov_invalid", got)
	}
}

func TestValidatePrefixMinROALen(t *testing.T) {
	s := &Rpki{}

	// VRP for /7 (too short, below min_vrp_v4)
	roa4 := make(VRPs)
	roa4[netip.MustParsePrefix("128.0.0.0/7")] = []VRP{
		{MaxLen: 24, ASN: 65001},
	}

	// /24 within /7 range - should NOT match (stops at /8)
	p := netip.MustParsePrefix("128.1.0.0/24")
	got := s.validatePrefix(roa4, nil, p, 65001)

	if got != rov_not_found {
		t.Errorf("should not check beyond minROALenV4, got %d", got)
	}
}

func TestValidatePrefixEmptyCache(t *testing.T) {
	s := &Rpki{}

	// Nil and empty caches
	tests := []struct {
		name string
		roa4 VRPs
		roa6 VRPs
	}{
		{"nil cache", nil, nil},
		{"empty map v4", VRPs{}, nil},
		{"empty map v6", nil, VRPs{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p4 := netip.MustParsePrefix("192.0.2.0/24")
			p6 := netip.MustParsePrefix("2001:db8::/32")

			got4 := s.validatePrefix(tt.roa4, nil, p4, 65001)
			got6 := s.validatePrefix(nil, tt.roa6, p6, 65001)

			if got4 != rov_not_found || got6 != rov_not_found {
				t.Errorf("empty cache should return NOT_FOUND")
			}
		})
	}
}
