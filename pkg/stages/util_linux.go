//go:build linux

package stages

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func tcp_md5(md5pass string) func(net, addr string, c syscall.RawConn) error {
	if len(md5pass) == 0 {
		return nil
	}

	return func(net, addr string, c syscall.RawConn) error {
		// setup tcp sig
		var key [80]byte
		l := copy(key[:], md5pass)
		sig := unix.TCPMD5Sig{
			Flags:     unix.TCP_MD5SIG_FLAG_PREFIX,
			Prefixlen: 0,
			Keylen:    uint16(l),
			Key:       key,
		}

		// addr family
		switch net {
		case "tcp6", "udp6", "ip6":
			sig.Addr.Family = unix.AF_INET6
		default:
			sig.Addr.Family = unix.AF_INET
		}

		// setsockopt
		var err error
		c.Control(func(fd uintptr) {
			b := *(*[unsafe.Sizeof(sig)]byte)(unsafe.Pointer(&sig))
			err = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_MD5SIG_EXT, string(b[:]))
		})
		return err
	}
}
