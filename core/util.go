package core

import (
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"

	"github.com/bgpfix/bgpfix/msg"
)

const (
	StyleNone      = ""
	StyleBlack     = "\033[30m"
	StyleRed       = "\033[31m"
	StyleGreen     = "\033[32m"
	StyleYellow    = "\033[33m"
	StyleBlue      = "\033[34m"
	StyleMagenta   = "\033[35m"
	StyleCyan      = "\033[36m"
	StyleWhite     = "\033[37m"
	StyleBold      = "\033[1m"
	StyleUnderline = "\033[4m"
	StyleReset     = "\033[0m"
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

// ParseEvents parses events in src and returns the result, or nil.
// If stage_defaults is given, events like "foobar" are translated to "foobar/stage_defaults[:]".
func ParseEvents(src []string, stage_defaults ...string) []string {
	var dst []string
	for _, event := range src {
		// special catch-all value?
		if event == "all" || event == "*" {
			dst = append(dst[:0], "*")
			continue
		}

		// split event into slash/dot.name
		slash, name, has_slash := strings.Cut(event, "/")
		if !has_slash {
			name = slash
			slash = ""
		}
		dot, name, has_dot := strings.Cut(name, ".")
		if !has_dot {
			name = dot
			dot = ""
		}

		// get name as UPPER / lower case
		UPPER := strings.ToUpper(name)
		lower := strings.ToLower(name)

		switch {
		case has_dot && has_slash:
			// eg. foo/bar.name -> foo/bar.NAME
			name = fmt.Sprintf("%s/%s.%s", slash, dot, UPPER)
		case has_dot:
			// eg. bar.name -> bgpfix/bar.NAME
			name = fmt.Sprintf("bgpfix/%s.%s", dot, UPPER)
		case has_slash:
			// eg. foo/name -> foo/name (specific stage event)
			name = fmt.Sprintf("%s/%s", slash, name)
		case name == lower && len(stage_defaults) > 0:
			// eg. foo -> foo/sds[0], foo/sds[1], etc. (default stage events)
			for _, sd := range stage_defaults {
				dst = append(dst, fmt.Sprintf("%s/%s", name, sd))
			}
			continue
		default:
			// eg. established -> bgpfix/pipe.ESTABLISHED
			name = fmt.Sprintf("bgpfix/pipe.%s", UPPER)
		}

		dst = append(dst, name)
	}

	return dst
}

func ParseTypes(src []string, dst []msg.Type) ([]msg.Type, error) {
	for _, t := range src {
		// skip empty types
		if len(t) == 0 {
			continue
		}

		// canonical name?
		typ, err := msg.TypeString(t)
		if err == nil {
			dst = append(dst, typ)
			continue
		}

		// a plain integer?
		tnum, err2 := strconv.ParseUint(t, 0, 8)
		if err2 == nil {
			dst = append(dst, msg.Type(tnum))
			continue
		}

		return dst, err
	}

	return dst, nil
}
