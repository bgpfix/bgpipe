//go:build openbsd

package stages

import (
	"syscall"
  "golang.org/x/sys/unix"
)

func tcp_md5(md5pass string) func(net, addr string, c syscall.RawConn) error {
	if len(md5pass) == 0 {
		return nil
	}

	return func(net, addr string, c syscall.RawConn) error {

    // * Check whether the tcpmd5 SA already exists
    // * If it doesn't, create a temporary file that can be used to load rules
    // * Execute ipsecctl -f /path/to/file to load the sa

		// setsockopt
		var err error
		c.Control(func(fd uintptr) {

      /*
      Future: 0x04 comes from https://github.com/openbsd/src/blob/master/sys/netinet/tcp.h#L217
      While it is unlikely to change, looking it up would be better rather than having it hardcoded.
      */

			err = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, 0x04, string("tcpmd5string"))
		})
		return err
	}
}
