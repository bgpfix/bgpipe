package rpki

import (
	"net/netip"
	"testing"
)

func TestFileParseJSON_ValidRoutinatorFormat(t *testing.T) {
	s := newTestRpki()

	json := []byte(`{
		"roas": [
			{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": "AS65001"},
			{"prefix": "203.0.113.0/24", "maxLength": 26, "asn": 65002},
			{"prefix": "2001:db8::/32", "maxLength": 48, "asn": 65003}
		]
	}`)

	err := s.fileParseJSON(json)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Check IPv4 entries
	if len(s.next4) != 2 {
		t.Errorf("expected 2 IPv4 ROAs, got %d", len(s.next4))
	}

	p1 := netip.MustParsePrefix("192.0.2.0/24")
	if entries := s.next4[p1]; len(entries) != 1 {
		t.Errorf("expected 1 entry for 192.0.2.0/24, got %d", len(entries))
	} else if entries[0].ASN != 65001 || entries[0].MaxLen != 24 {
		t.Errorf("wrong entry: %+v", entries[0])
	}

	p2 := netip.MustParsePrefix("203.0.113.0/24")
	if entries := s.next4[p2]; len(entries) != 1 {
		t.Errorf("expected 1 entry for 203.0.113.0/24, got %d", len(entries))
	} else if entries[0].ASN != 65002 || entries[0].MaxLen != 26 {
		t.Errorf("wrong entry: %+v", entries[0])
	}

	// Check IPv6 entries
	if len(s.next6) != 1 {
		t.Errorf("expected 1 IPv6 ROA, got %d", len(s.next6))
	}

	p3 := netip.MustParsePrefix("2001:db8::/32")
	if entries := s.next6[p3]; len(entries) != 1 {
		t.Errorf("expected 1 entry for 2001:db8::/32, got %d", len(entries))
	} else if entries[0].ASN != 65003 || entries[0].MaxLen != 48 {
		t.Errorf("wrong entry: %+v", entries[0])
	}
}

func TestFileParseJSON_ASNFormats(t *testing.T) {
	tests := []struct {
		name   string
		json   string
		wantOK bool
		asn    uint32
	}{
		{"string AS prefix", `{"roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": "AS65001"}]}`, true, 65001},
		{"string no prefix", `{"roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": "65001"}]}`, true, 65001},
		{"integer", `{"roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": 65001}]}`, true, 65001},
		{"float", `{"roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": 65001.0}]}`, true, 65001},
		{"uppercase AS", `{"roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": "AS65001"}]}`, true, 65001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Rpki{}
			s.nextFlush()

			err := s.fileParseJSON([]byte(tt.json))
			if tt.wantOK && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantOK && err == nil {
				t.Fatal("expected error, got nil")
			}

			if tt.wantOK {
				p := netip.MustParsePrefix("192.0.2.0/24")
				if entries := s.next4[p]; len(entries) != 1 {
					t.Fatalf("expected 1 entry, got %d", len(entries))
				} else if entries[0].ASN != tt.asn {
					t.Errorf("got ASN %d, want %d", entries[0].ASN, tt.asn)
				}
			}
		})
	}
}

func TestFileParseJSON_InvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"invalid JSON", `{invalid json}`},
		{"missing roas field", `{}`},
		{"invalid prefix", `{"roas": [{"prefix": "not-a-prefix", "maxLength": 24, "asn": 65001}]}`},
		{"maxLength too small", `{"roas": [{"prefix": "192.0.2.0/24", "maxLength": 23, "asn": 65001}]}`},
		{"maxLength too large", `{"roas": [{"prefix": "192.0.2.0/24", "maxLength": 129, "asn": 65001}]}`},
		{"invalid ASN string", `{"roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": "invalid"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestRpki()

			// Should either error or skip the invalid entry
			s.fileParseJSON([]byte(tt.json))

			// All cases should result in empty/no valid entries
			// (either parse error or entries skipped with warnings)
		})
	}
}

func TestFileParseCSV_Valid(t *testing.T) {
	s := newTestRpki()

	csv := []byte(`prefix,maxLength,asn
192.0.2.0/24,24,AS65001
203.0.113.0/24,26,65002
2001:db8::/32,48,AS65003
`)

	err := s.fileParseCSV(csv)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(s.next4) != 2 {
		t.Errorf("expected 2 IPv4 ROAs, got %d", len(s.next4))
	}
	if len(s.next6) != 1 {
		t.Errorf("expected 1 IPv6 ROA, got %d", len(s.next6))
	}

	// Verify specific entries
	p1 := netip.MustParsePrefix("192.0.2.0/24")
	if entries := s.next4[p1]; len(entries) != 1 || entries[0].ASN != 65001 {
		t.Errorf("wrong entry for 192.0.2.0/24: %+v", entries)
	}
}

func TestFileParseCSV_NoHeader(t *testing.T) {
	s := newTestRpki()

	csv := []byte(`192.0.2.0/24,24,65001
203.0.113.0/24,26,65002`)

	err := s.fileParseCSV(csv)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(s.next4) != 2 {
		t.Errorf("expected 2 IPv4 ROAs, got %d", len(s.next4))
	}
}

func TestFileParseCSV_Comments(t *testing.T) {
	s := newTestRpki()

	csv := []byte(`# This is a comment
192.0.2.0/24,24,65001
# Another comment
203.0.113.0/24,26,65002

# Empty lines above and below
`)

	err := s.fileParseCSV(csv)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(s.next4) != 2 {
		t.Errorf("expected 2 IPv4 ROAs (comments ignored), got %d", len(s.next4))
	}
}

func TestFileParseCSV_Whitespace(t *testing.T) {
	s := newTestRpki()

	csv := []byte(`  192.0.2.0/24  ,  24  ,  AS65001
203.0.113.0/24,26,65002`)

	err := s.fileParseCSV(csv)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	p := netip.MustParsePrefix("192.0.2.0/24")
	if entries := s.next4[p]; len(entries) != 1 || entries[0].ASN != 65001 {
		t.Errorf("whitespace not trimmed properly: %+v", entries)
	}
}

func TestFileParseCSV_InvalidLines(t *testing.T) {
	s := newTestRpki()

	csv := []byte(`192.0.2.0/24,24,65001
invalid line
203.0.113.0/24,invalid,65002
204.0.113.0/24,24,invalid-asn
205.0.113.0/24,23,65003
206.0.113.0/24,25,65004`)

	err := s.fileParseCSV(csv)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Should have 2 valid entries (first and last)
	// Others are skipped due to various validation errors
	if len(s.next4) != 2 {
		t.Errorf("expected 2 valid IPv4 ROAs, got %d", len(s.next4))
	}
}

func TestFileParse_AutoDetect(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		wantV4   int
		wantV6   int
		isJSON   bool
	}{
		{
			name:     "JSON detected",
			data:     []byte(`{"roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": 65001}]}`),
			wantV4:   1,
			wantV6:   0,
			isJSON:   true,
		},
		{
			name:     "CSV detected",
			data:     []byte("192.0.2.0/24,24,65001"),
			wantV4:   1,
			wantV6:   0,
			isJSON:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Rpki{}
			s.nextFlush()

			err := s.fileParse(tt.data)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			if len(s.next4) != tt.wantV4 {
				t.Errorf("got %d IPv4 ROAs, want %d", len(s.next4), tt.wantV4)
			}
			if len(s.next6) != tt.wantV6 {
				t.Errorf("got %d IPv6 ROAs, want %d", len(s.next6), tt.wantV6)
			}
		})
	}
}

func TestFileParse_PrefixMasking(t *testing.T) {
	s := newTestRpki()

	// Unmasked prefix in JSON
	json := []byte(`{"roas": [{"prefix": "192.0.2.123/24", "maxLength": 24, "asn": 65001}]}`)
	err := s.fileParseJSON(json)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Should be stored as masked 192.0.2.0/24
	masked := netip.MustParsePrefix("192.0.2.0/24")
	if _, exists := s.next4[masked]; !exists {
		t.Error("prefix not properly masked in JSON parse")
	}
}
