//go:build linux

package util

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TcpMd5 returns a control func installing the TCP-MD5 signature password.
func TcpMd5(md5pass string) ControlFunc {
	if len(md5pass) == 0 {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		var key [80]byte
		l := copy(key[:], md5pass)
		sig := unix.TCPMD5Sig{
			Flags:     unix.TCP_MD5SIG_FLAG_PREFIX,
			Prefixlen: 0,
			Keylen:    uint16(l),
			Key:       key,
		}
		if isV6(network, address) {
			sig.Addr.Family = unix.AF_INET6
		} else {
			sig.Addr.Family = unix.AF_INET
		}

		var err error
		c.Control(func(fd uintptr) {
			b := *(*[unsafe.Sizeof(sig)]byte)(unsafe.Pointer(&sig))
			err = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_MD5SIG_EXT, string(b[:]))
		})
		return err
	}
}

// Transparent returns a control func enabling IP_TRANSPARENT (Linux TPROXY):
// it lets a listener accept connections destined to a foreign address, and a
// dialer bind to (spoof) a non-local source address.
func Transparent() ControlFunc {
	return func(network, address string, c syscall.RawConn) error {
		var err error
		c.Control(func(fd uintptr) {
			if isV6(network, address) {
				err = unix.SetsockoptInt(int(fd), unix.SOL_IPV6, unix.IPV6_TRANSPARENT, 1)
			} else {
				err = unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_TRANSPARENT, 1)
			}
		})
		return err
	}
}

// Ttl returns a control func setting the outgoing IP TTL / hop limit
// (0 leaves the kernel default; 255 satisfies GTSM / RFC 5082).
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
