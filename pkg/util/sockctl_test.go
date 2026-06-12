package util

import (
	"fmt"
	"reflect"
	"syscall"
	"testing"
)

func TestChain(t *testing.T) {
	var order []int
	mk := func(n int, fail bool) ControlFunc {
		return func(network, address string, c syscall.RawConn) error {
			order = append(order, n)
			if fail {
				return fmt.Errorf("boom %d", n)
			}
			return nil
		}
	}

	// only nils collapses to nil (no control func installed)
	if Chain(nil, nil) != nil {
		t.Fatal("Chain(nil, nil) should be nil")
	}

	// runs in order and short-circuits on the first error
	order = nil
	err := Chain(nil, mk(1, false), mk(2, true), mk(3, false))("tcp", "", nil)
	if err == nil {
		t.Fatal("expected error from chained func")
	}
	if !reflect.DeepEqual(order, []int{1, 2}) {
		t.Fatalf("order = %v, want [1 2]", order)
	}
}

func TestIsV6(t *testing.T) {
	cases := []struct {
		network, address string
		want             bool
	}{
		{"tcp4", "1.2.3.4:179", false},
		{"tcp6", "[::1]:179", true},
		{"tcp", "1.2.3.4:179", false},
		{"tcp", "[2001:db8::1]:179", true},
		{"tcp", "2001:db8::1", true},
		{"tcp", "::ffff:1.2.3.4", false}, // v4-mapped
		{"tcp", "garbage", false},
	}
	for _, c := range cases {
		if got := isV6(c.network, c.address); got != c.want {
			t.Errorf("isV6(%q, %q) = %v, want %v", c.network, c.address, got, c.want)
		}
	}
}

func TestTtlNoop(t *testing.T) {
	if Ttl(0) != nil {
		t.Fatal("Ttl(0) should be nil")
	}
	if Ttl(64) == nil {
		t.Fatal("Ttl(64) should return a control func")
	}
}
