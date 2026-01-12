package rpki

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bgpfix/bgpipe/pkg/util"
)

// fileRun does initial load and polls the file for changes
func (s *Rpki) fileRun() {
	// first load
	err := s.fileLoad()
	if err != nil {
		s.Fatal().Err(err).Msg("could not load the ROA file")
	} else {
		util.Close(s.roaReady)
	}

	// keep polling
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := s.fileLoad(); err != nil {
				s.Err(err).Msg("failed to re-load the ROA file")
			}
		case <-s.Ctx.Done():
			return
		}
	}
}

// fileLoad loads ROA data from file
func (s *Rpki) fileLoad() error {
	// stat file, check mod time
	fi, err := os.Stat(s.file)
	if err != nil {
		return err
	}
	if !fi.ModTime().After(s.fileMod) {
		return nil
	}

	// read file, check contents
	data, err := os.ReadFile(s.file)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	if hash == s.fileHash {
		return nil
	}

	// restart from scratch
	s.nextFlush()
	if err := s.fileParse(data); err != nil {
		return err
	}

	// apply
	s.nextApply()
	s.fileMod = fi.ModTime()
	s.fileHash = hash

	return nil
}

// fileParse parses ROA data from JSON or CSV
func (s *Rpki) fileParse(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return s.fileParseJSON(data)
	} else {
		return s.fileParseCSV(data)
	}
}

// fileParseJSON parses Routinator-style JSON
// Format: {"roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": "AS65001"}, ...]}
func (s *Rpki) fileParseJSON(data []byte) error {
	var doc struct {
		ROAs []struct {
			Prefix    string `json:"prefix"`
			MaxLength int    `json:"maxLength"`
			ASN       any    `json:"asn"` // can be string "AS65001" or int 65001
		} `json:"roas"`
	}

	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("JSON parse error: %w", err)
	}

	for _, roa := range doc.ROAs {
		prefix, err := netip.ParsePrefix(roa.Prefix)
		if err != nil {
			s.Warn().Str("prefix", roa.Prefix).Msg("invalid prefix, skipping")
			continue
		}
		prefix = prefix.Masked()

		// Parse ASN (handle both "AS65001" and 65001)
		var asn uint32
		switch v := roa.ASN.(type) {
		case string:
			v = strings.ToLower(v)
			v = strings.TrimPrefix(v, "as")
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				s.Warn().Str("asn", fmt.Sprint(roa.ASN)).Msg("invalid ASN, skipping")
				continue
			}
			asn = uint32(n)
		case int:
			asn = uint32(v)
		case float64:
			asn = uint32(v)
		default:
			s.Warn().Str("asn", fmt.Sprint(roa.ASN)).Msg("invalid ASN type, skipping")
			continue
		}

		// check MaxLength
		if roa.MaxLength < prefix.Bits() || roa.MaxLength > 128 {
			s.Warn().Str("prefix", roa.Prefix).Int("maxLength", roa.MaxLength).Msg("invalid MaxLength, skipping")
			continue
		}

		s.nextAdd(prefix, uint8(roa.MaxLength), asn)
	}

	return nil
}

// fileParseCSV parses CSV format: prefix,maxLength,asn
func (s *Rpki) fileParseCSV(data []byte) error {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}

		// Skip header
		if i == 0 && strings.Contains(strings.ToLower(line), "prefix") {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			s.Warn().Int("line", i+1).Msg("invalid CSV line, skipping")
			continue
		}

		prefix, err := netip.ParsePrefix(strings.TrimSpace(parts[0]))
		if err != nil {
			s.Warn().Int("line", i+1).Str("prefix", parts[0]).Msg("invalid prefix, skipping")
			continue
		}
		prefix = prefix.Masked()

		maxLen, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || maxLen < prefix.Bits() || maxLen > 128 {
			s.Warn().Int("line", i+1).Msg("invalid maxLength, skipping")
			continue
		}

		asnStr := strings.ToLower(strings.TrimSpace(parts[2]))
		asnStr = strings.TrimPrefix(asnStr, "as")
		asn, err := strconv.ParseUint(asnStr, 10, 32)
		if err != nil {
			s.Warn().Int("line", i+1).Msg("invalid ASN, skipping")
			continue
		}

		s.nextAdd(prefix, uint8(maxLen), uint32(asn))
	}

	return nil
}
