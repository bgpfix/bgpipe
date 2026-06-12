//go:build openbsd

package util

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

// TcpMd5 enables TCP-MD5 on the socket. On OpenBSD the key itself lives in the
// kernel SADB (see ipsec.conf(5) tcpmd5, loaded via ipsecctl); a non-empty
// password here only flips the TCP_MD5SIG socket flag on.
func TcpMd5(md5pass string) ControlFunc {
	if len(md5pass) == 0 {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		var err error
		c.Control(func(fd uintptr) {
			err = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_MD5SIG, 1)
		})
		return err
	}
}

// Transparent is Linux-only (OpenBSD uses pf divert-to instead).
func Transparent() ControlFunc {
	return func(network, address string, c syscall.RawConn) error {
		return fmt.Errorf("transparent mode is only supported on Linux")
	}
}

// Ttl returns a control func setting the outgoing IP TTL / hop limit.
func Ttl(ttl int) ControlFunc {
	if ttl <= 0 {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		var err error
		c.Control(func(fd uintptr) {
			if isV6(network, address) {
				err = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, ttl)
			} else {
				err = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TTL, ttl)
			}
		})
		return err
	}
}
