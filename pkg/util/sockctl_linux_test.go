//go:build linux

package util

import (
	"context"
	"net"
	"testing"

	"golang.org/x/sys/unix"
)

// TestTtlControl verifies Ttl actually sets IP_TTL on a real socket (no root needed).
func TestTtlControl(t *testing.T) {
	lc := net.ListenConfig{Control: Ttl(55)}
	l, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	rc, err := l.(*net.TCPListener).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}

	var got int
	var gerr error
	rc.Control(func(fd uintptr) {
		got, gerr = unix.GetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TTL)
	})
	if gerr != nil {
		t.Fatal(gerr)
	}
	if got != 55 {
		t.Fatalf("IP_TTL = %d, want 55", got)
	}
}

// TestNoMPTCP verifies listeners are plain TCP, not MPTCP (see godebug in go.mod).
func TestNoMPTCP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	rc, err := l.(*net.TCPListener).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}

	var got int
	var gerr error
	rc.Control(func(fd uintptr) {
		got, gerr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PROTOCOL)
	})
	if gerr != nil {
		t.Fatal(gerr)
	}
	if got != unix.IPPROTO_TCP {
		t.Fatalf("SO_PROTOCOL = %d, want %d (TCP)", got, unix.IPPROTO_TCP)
	}
}
