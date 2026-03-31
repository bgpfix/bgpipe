package rpki

import (
	"net/netip"
	"testing"
)

func TestNextAddBasic(t *testing.T) {
	s := newTestRpkiSimple()

	// Test IPv4 addition
	p4 := netip.MustParsePrefix("192.0.2.0/24")
	s.nextVRP(true, p4, 24, 65001)

	if len(s.next4) != 1 {
		t.Fatalf("expected 1 IPv4 VRP, got %d", len(s.next4))
	}
	if entries := s.next4[p4]; len(entries) != 1 {
		t.Fatalf("expected 1 entry for prefix, got %d", len(entries))
	}
	if entry := s.next4[p4][0]; entry.ASN != 65001 || entry.MaxLen != 24 {
		t.Errorf("wrong entry: ASN=%d MaxLen=%d", entry.ASN, entry.MaxLen)
	}

	// Test IPv6 addition
	p6 := netip.MustParsePrefix("2001:db8::/32")
	s.nextVRP(true, p6, 48, 65002)

	if len(s.next6) != 1 {
		t.Fatalf("expected 1 IPv6 VRP, got %d", len(s.next6))
	}
	if entries := s.next6[p6]; len(entries) != 1 {
		t.Fatalf("expected 1 entry for prefix, got %d", len(entries))
	}
}

func TestNextAddDuplicates(t *testing.T) {
	s := newTestRpkiSimple()

	p := netip.MustParsePrefix("192.0.2.0/24")

	// Add same VRP twice
	s.nextVRP(true, p, 24, 65001)
	s.nextVRP(true, p, 24, 65001)

	if len(s.next4[p]) != 1 {
		t.Errorf("expected 1 entry (duplicate ignored), got %d", len(s.next4[p]))
	}
}

func TestNextAddMultipleOrigins(t *testing.T) {
	s := newTestRpkiSimple()

	p := netip.MustParsePrefix("192.0.2.0/24")

	// Same prefix, different ASNs (MOAS scenario)
	s.nextVRP(true, p, 24, 65001)
	s.nextVRP(true, p, 24, 65002)
	s.nextVRP(true, p, 25, 65001) // Same prefix, different maxLen

	if len(s.next4[p]) != 3 {
		t.Errorf("expected 3 entries, got %d", len(s.next4[p]))
	}
}

func TestNextDel(t *testing.T) {
	s := newTestRpkiSimple()

	p := netip.MustParsePrefix("192.0.2.0/24")

	// Add then delete
	s.nextVRP(true, p, 24, 65001)
	s.nextVRP(true, p, 24, 65002)
	s.nextVRP(false, p, 24, 65001)

	entries := s.next4[p]
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after delete, got %d", len(entries))
	}
	if entries[0].ASN != 65002 {
		t.Errorf("wrong entry remaining: ASN=%d", entries[0].ASN)
	}
}

func TestNextDelNonExistent(t *testing.T) {
	s := newTestRpkiSimple()

	p := netip.MustParsePrefix("192.0.2.0/24")
	s.nextVRP(true, p, 24, 65001)

	// Delete non-existent entry (should be no-op)
	s.nextVRP(false, p, 24, 65999)

	if len(s.next4[p]) != 1 {
		t.Errorf("expected 1 entry (delete ignored), got %d", len(s.next4[p]))
	}
}

func TestNextApply(t *testing.T) {
	s := newTestRpki()
	s.vrp_done = make(chan bool)

	// Add some VRPs
	s.nextVRP(true, netip.MustParsePrefix("192.0.2.0/24"), 24, 65001)
	s.nextVRP(true, netip.MustParsePrefix("2001:db8::/32"), 48, 65002)

	// Apply (publishes next -> current)
	s.nextApply()

	// Check current caches were updated
	v4 := s.vrp4.Load()
	v6 := s.vrp6.Load()

	if len(*v4) != 1 {
		t.Errorf("expected 1 IPv4 VRP in current, got %d", len(*v4))
	}
	if len(*v6) != 1 {
		t.Errorf("expected 1 IPv6 VRP in current, got %d", len(*v6))
	}

	// Check next was cloned (for incremental updates)
	if len(s.next4) != 1 || len(s.next6) != 1 {
		t.Errorf("expected next caches to be cloned, got v4=%d v6=%d",
			len(s.next4), len(s.next6))
	}
}

func TestNextVRP_InvalidMaxLength(t *testing.T) {
	s := newTestRpki()

	// maxLen=33 exceeds IPv4 max of 32 → should be rejected
	p4 := netip.MustParsePrefix("192.0.2.0/24")
	s.nextVRP(true, p4, 33, 65001)
	if len(s.next4[p4]) != 0 {
		t.Error("maxLen=33 should be rejected for IPv4")
	}

	// maxLen=129 exceeds IPv6 max of 128 → should be rejected
	p6 := netip.MustParsePrefix("2001:db8::/32")
	s.nextVRP(true, p6, 129, 65002)
	if len(s.next6[p6]) != 0 {
		t.Error("maxLen=129 should be rejected for IPv6")
	}

	// maxLen < prefix length → should be rejected
	s.nextVRP(true, p4, 20, 65001)
	if len(s.next4[p4]) != 0 {
		t.Error("maxLen < prefix length should be rejected")
	}

	// valid maxLen should be accepted
	s.nextVRP(true, p4, 32, 65001)
	if len(s.next4[p4]) != 1 {
		t.Error("maxLen=32 should be accepted for /24 IPv4")
	}
}

func TestPrefixMasking(t *testing.T) {
	s := newTestRpkiSimple()

	// Add unmasked prefix (should be masked automatically)
	p := netip.MustParsePrefix("192.0.2.123/24")
	s.nextVRP(true, p, 24, 65001)

	// Should be stored as masked prefix
	masked := netip.MustParsePrefix("192.0.2.0/24")
	if _, exists := s.next4[masked]; !exists {
		t.Error("prefix was not properly masked")
	}
	if _, exists := s.next4[p]; exists && p != masked {
		t.Error("unmasked prefix was stored")
	}
}
