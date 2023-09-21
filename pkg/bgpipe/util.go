package bgpipe

import "net/netip"

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
