package bgpipe

import (
	"math"
	"net/netip"
	"strconv"
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

func IsBind(v string) bool {
	if len(v) < 2 {
		return false
	}
	if v[0] != ':' {
		return false
	}
	p, err := strconv.Atoi(v[1:])
	if err != nil || p <= 0 || p > math.MaxUint16 {
		return false
	}
	return true
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
		// special values
		if et == "all" || et == "*" {
			events[i] = "*"
			continue
		}

		has_dot := strings.IndexByte(et, '.') > 0
		has_slash := strings.IndexByte(et, '/') > 0

		if has_dot && has_slash {
			// fully specified, done
		} else if !has_slash {
			if !has_dot {
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
