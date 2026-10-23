package stages

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bgpfix/bgpipe/core"
)

// runBgpipe runs bgpipe with given CLI args, as if from the command line
func runBgpipe(t *testing.T, args ...string) {
	t.Helper()
	defer func(a []string) { os.Args = a }(os.Args)
	os.Args = append([]string{"bgpipe", "--log", "disabled", "-e", ""}, args...)
	if err := core.NewBgpipe("test", Repo).Run(); err != nil {
		t.Fatalf("bgpipe %v: %v", args, err)
	}
}

// TestWrite checks that the write stage writes all messages, in order, incl. compressed output
func TestWrite(t *testing.T) {
	const N = 10000
	dir := t.TempDir()

	// input: N KEEPALIVEs, with seq numbers 1..N
	var in strings.Builder
	for i := 1; i <= N; i++ {
		fmt.Fprintf(&in, `["R",%d,"2026-01-01T00:00:00.000","KEEPALIVE",null,null]`+"\n", i)
	}
	inpath := filepath.Join(dir, "in.json")
	if err := os.WriteFile(inpath, []byte(in.String()), 0644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"out.json", "out.json.gz"} {
		t.Run(name, func(t *testing.T) {
			outpath := filepath.Join(dir, name)
			runBgpipe(t, "--", "read", inpath, "--", "write", outpath)

			fh, err := os.Open(outpath)
			if err != nil {
				t.Fatal(err)
			}
			defer fh.Close()
			var r io.Reader = fh
			if strings.HasSuffix(name, ".gz") {
				if r, err = gzip.NewReader(fh); err != nil {
					t.Fatal(err)
				}
			}

			// expect the same N messages, in order
			sc := bufio.NewScanner(r)
			i := 0
			for sc.Scan() {
				i++
				if want := fmt.Sprintf(`["R",%d,`, i); !strings.HasPrefix(sc.Text(), want) {
					t.Fatalf("line %d: got %q, want prefix %q", i, sc.Text(), want)
				}
			}
			if err := sc.Err(); err != nil {
				t.Fatal(err)
			}
			if i != N {
				t.Fatalf("got %d lines, want %d", i, N)
			}
		})
	}
}
