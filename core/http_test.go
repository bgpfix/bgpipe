package core

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPprofStop is a regression test for the standalone --pprof server
// leaking its listener: a second Run() on the same address must not fail
// with "address already in use" after stopHTTP().
func TestPprofStop(t *testing.T) {
	// pick a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	for i := range 2 {
		b := NewBgpipe("test")
		b.K.Set("pprof", addr)
		require.NoError(t, b.configureHTTP(), "run %d", i)
		b.stopHTTP()
	}
}
