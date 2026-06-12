package util

import (
	"net"
	"net/netip"
	"strings"
	"syscall"
)

// ControlFunc is a net.Dialer / net.ListenConfig socket control callback.
type ControlFunc = func(network, address string, c syscall.RawConn) error

// Chain composes control funcs into one, skipping nils and stopping on the first error.
func Chain(fns ...ControlFunc) ControlFunc {
	var nn []ControlFunc
	for _, f := range fns {
		if f != nil {
			nn = append(nn, f)
		}
	}
	switch len(nn) {
	case 0:
		return nil
	case 1:
		return nn[0]
	}
	return func(network, address string, c syscall.RawConn) error {
		for _, f := range nn {
			if err := f(network, address, c); err != nil {
				return err
			}
		}
		return nil
	}
}

// isV6 reports whether network/address refers to IPv6.
func isV6(network, address string) bool {
	if strings.HasSuffix(network, "6") {
		return true
	}
	if strings.HasSuffix(network, "4") {
		return false
	}
	host := address
	if h, _, err := net.SplitHostPort(address); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Is6() && !ip.Is4In6()
}
