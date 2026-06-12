//go:build !linux && !openbsd

package util

import (
	"fmt"
	"syscall"
)

func TcpMd5(md5pass string) ControlFunc {
	if len(md5pass) == 0 {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		return fmt.Errorf("no TCP-MD5 support on this platform")
	}
}

func Transparent() ControlFunc {
	return func(network, address string, c syscall.RawConn) error {
		return fmt.Errorf("transparent mode is only supported on Linux")
	}
}

func Ttl(ttl int) ControlFunc {
	if ttl <= 0 {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		return fmt.Errorf("no TTL control on this platform")
	}
}
