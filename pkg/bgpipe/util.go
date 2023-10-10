package bgpipe

import (
	"fmt"
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

		// split slash/dot.event
		slash, et, has_slash := strings.Cut(et, "/")
		if !has_slash {
			et = slash
			slash = ""
		}
		dot, et, has_dot := strings.Cut(et, ".")
		if !has_dot {
			et = dot
			dot = ""
		}
		et_lower := strings.ToLower(et)
		et_upper := strings.ToUpper(et)

		switch {
		case has_dot && has_slash:
			et = fmt.Sprintf("%s/%s.%s", slash, dot, et_upper)
		case has_dot:
			et = fmt.Sprintf("bgpfix/%s.%s", dot, et_upper)
		case has_slash:
			et = fmt.Sprintf("%s/%s", slash, et_upper) // stage event
		default:
			if et == et_lower {
				et = fmt.Sprintf("%s/READY", et) // stage ready
			} else {
				et = fmt.Sprintf("bgpfix/pipe.%s", et_upper)
			}
		}

		b.Trace().Msgf("parseEvents(): '%s' -> '%s'", events[i], et)
		events[i] = et
	}

	return events
}
