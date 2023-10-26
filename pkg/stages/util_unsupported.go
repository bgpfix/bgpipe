//go:build !linux

package stages

import (
	"fmt"
	"syscall"
)

func tcp_md5(md5pass string) func(net, addr string, c syscall.RawConn) error {
	if len(md5pass) == 0 {
		return nil
	}

	return func(net, addr string, c syscall.RawConn) error {
		return fmt.Errorf("no TCP-MD5 support on this platform")
	}
}
