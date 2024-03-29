package core

import (
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"

	"github.com/knadh/koanf/v2"
)

func IsAddr(v string) bool {
	if v == "localhost" || strings.HasPrefix(v, "localhost:") {
		return true
	}
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
func (b *Bgpipe) parseEvents(k *koanf.Koanf, key string, sds ...string) []string {
	input := k.Strings(key)
	if len(input) == 0 {
		return nil
	}

	// rewrite
	var output []string
	for _, et := range input {
		// special values
		if et == "all" || et == "*" {
			output = append(output, "*")
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
			et = fmt.Sprintf("%s/%s", slash, et) // stage event
		default:
			// stage name + stage defaults?
			if et == et_lower && len(sds) > 0 {
				for _, sd := range sds {
					output = append(output, fmt.Sprintf("%s/%s", et, sd))
				}
				continue
			}

			et = fmt.Sprintf("bgpfix/pipe.%s", et_upper)
		}

		output = append(output, et)
	}

	b.Trace().Msgf("parseEvents(): %s -> %s", input, output)
	return output
}
