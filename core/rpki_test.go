package core

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRpkiFileLoadAndReload(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rpki.json")
	require.NoError(t, os.WriteFile(file,
		[]byte(`{"roas":[{"prefix":"192.0.2.0/24","maxLength":24,"asn":65001}],"aspas":[]}`),
		0o644))

	b := NewBgpipe("test")
	cache := b.UseRpki()
	require.Same(t, cache, b.UseRpki(), "UseRpki must return the same cache")

	b.Rpki.source = file
	require.NoError(t, b.Rpki.fileLoad())
	vrps4, _, aspas := cache.Sizes()
	require.Equal(t, 1, vrps4)
	require.Equal(t, 0, aspas)

	// no change → no-op
	require.NoError(t, b.Rpki.fileLoad())

	newData := `{
		"roas": [
			{"prefix":"192.0.2.0/24","maxLength":24,"asn":65001},
			{"prefix":"10.0.0.0/8","maxLength":24,"asn":64512}
		],
		"aspas": [{"customer_asid":65010,"provider_asids":[65001]}]
	}`
	require.NoError(t, os.WriteFile(file, []byte(newData), 0o644))
	// NB: fileLoad short-circuits unless mtime advanced
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(file, future, future))

	require.NoError(t, b.Rpki.fileLoad())
	vrps4, _, aspas = cache.Sizes()
	require.Equal(t, 2, vrps4)
	require.Equal(t, 1, aspas)
}

// TestRpkiRtrSource runs the core RTR loop against a minimal in-process
// RTR v2 server, and checks the shared cache gets fed and applied.
func TestRpkiRtrSource(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// read the Reset Query
		buf := make([]byte, 8)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}

		// Cache Response (v2, session 7)
		conn.Write([]byte{2, 3, 0, 7, 0, 0, 0, 8})
		// IPv4 Prefix: announce 192.0.2.0/24-24 AS65001
		conn.Write([]byte{2, 4, 0, 0, 0, 0, 0, 20,
			1, 24, 24, 0,
			192, 0, 2, 0,
			0, 0, 0xfd, 0xe9})
		// ASPA: announce CAS 65010 → provider 65001 (flags in header byte 2)
		conn.Write([]byte{2, 11, 1, 0, 0, 0, 0, 16,
			0, 0, 0xfd, 0xf2,
			0, 0, 0xfd, 0xe9})
		// End of Data (session 7, serial 1, v1/v2 intervals)
		conn.Write([]byte{2, 7, 0, 7, 0, 0, 0, 24,
			0, 0, 0, 1,
			0, 0, 14, 16,
			0, 0, 2, 88,
			0, 0, 28, 32})

		// hold the connection open until the client disconnects
		io.Copy(io.Discard, conn)
	}()

	b := NewBgpipe("test")
	cache := b.UseRpki()
	b.K.Set("rpki", ln.Addr().String())
	b.K.Set("rpki-refresh", time.Hour)
	b.K.Set("rpki-retry", 10*time.Minute)
	require.NoError(t, b.Rpki.Start())
	defer b.Cancel(errors.New("test done"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, cache.WaitReady(ctx))

	vrps4, vrps6, aspas := cache.Sizes()
	require.Equal(t, 1, vrps4)
	require.Equal(t, 0, vrps6)
	require.Equal(t, 1, aspas)
	require.Equal(t, []uint32{65001}, cache.ASPAs()[65010])
}

// TestRpkiUrlSource checks the HTTP(S) source: initial fetch and re-fetch.
func TestRpkiUrlSource(t *testing.T) {
	data := `{"roas":[{"prefix":"192.0.2.0/24","maxLength":24,"asn":65001}],"aspas":[{"customer_asid":65010,"providers":[65001]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(data))
	}))
	defer srv.Close()

	b := NewBgpipe("test")
	cache := b.UseRpki()
	b.Rpki.source = srv.URL
	require.NoError(t, b.Rpki.urlFetch())

	vrps4, _, aspas := cache.Sizes()
	require.Equal(t, 1, vrps4)
	require.Equal(t, 1, aspas)

	// re-fetch with new data replaces the cache
	data = `{"roas":[{"prefix":"192.0.2.0/24","maxLength":24,"asn":65001},{"prefix":"10.0.0.0/8","maxLength":24,"asn":64512}],"aspas":[]}`
	require.NoError(t, b.Rpki.urlFetch())
	vrps4, _, aspas = cache.Sizes()
	require.Equal(t, 2, vrps4)
	require.Equal(t, 0, aspas)
}

func TestRpkiUrlSourceBroken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	b := NewBgpipe("test")
	b.UseRpki()
	b.Rpki.source = srv.URL
	require.Error(t, b.Rpki.urlFetch())
}

func TestRpkiFileLoadBroken(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rpki.json")
	require.NoError(t, os.WriteFile(file, []byte(`{broken`), 0o644))

	b := NewBgpipe("test")
	b.UseRpki()
	b.Rpki.source = file
	require.Error(t, b.Rpki.fileLoad())
}
