package bgpipe

import (
	"net/netip"
	"strings"

	"github.com/knadh/koanf/v2"
)

func IsAddr(v string) bool {
	if _, err := netip.ParseAddrPort(v); err == nil {
		return true
	}
	if _, err := netip.ParseAddr(v); err == nil {
		return true
	}
	// TODO: dns.name[:port]
	return false
}

func IsFile(v string) bool {
	switch v[0] {
	case '.', '/':
		return true
	default:
		return false
	}
}

// parseEvents returns events from given koanf key, or nil if none found
func (b *Bgpipe) parseEvents(k *koanf.Koanf, key string) []string {
	events := k.Strings(key)
	if len(events) == 0 {
		return nil
	}

	// rewrite
	for i, et := range events {
		// special value
		if et == "*" {
			continue
		}

		has_pkg := strings.IndexByte(et, '.') > 0
		has_lib := strings.IndexByte(et, '/') > 0

		if has_pkg && has_lib {
			// fully specified, done
		} else if !has_lib {
			if !has_pkg {
				et = "bgpfix/pipe." + strings.ToUpper(et)
			} else {
				et = "bgpfix/" + et
			}
		} else {
			// has lib, take as-is
		}

		events[i] = et
	}

	return events
}
