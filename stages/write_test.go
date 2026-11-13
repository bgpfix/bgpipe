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

// tryBgpipe runs bgpipe with given CLI args, as if from the command line
func tryBgpipe(args ...string) error {
	defer func(a []string) { os.Args = a }(os.Args)
	os.Args = append([]string{"bgpipe", "--log", "disabled", "-e", ""}, args...)
	return core.NewBgpipe("test", Repo).Run()
}

// runBgpipe is tryBgpipe that fails the test on error
func runBgpipe(t *testing.T, args ...string) {
	t.Helper()
	if err := tryBgpipe(args...); err != nil {
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

// TestWriteModes checks that --append and --create act on the target itself,
// and that a user's *.tmp target is never treated as our temporary file
func TestWriteModes(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	msg := `["R",1,"2026-01-01T00:00:00.000","KEEPALIVE",null,{}]` + "\n"
	if err := os.WriteFile(in, []byte(msg), 0644); err != nil {
		t.Fatal(err)
	}
	read := func(p string) string {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// --append keeps existing contents, twice
	out := filepath.Join(dir, "app.json")
	if err := os.WriteFile(out, []byte("existing\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runBgpipe(t, "--", "read", in, "--", "write", "--append", out)
	runBgpipe(t, "--", "read", in, "--", "write", "--append", out)
	if got, want := read(out), "existing\n"+msg+msg; got != want {
		t.Fatalf("--append: got %q, want %q", got, want)
	}

	// --create refuses an existing file and leaves it unchanged
	out = filepath.Join(dir, "create.json")
	if err := os.WriteFile(out, []byte("existing\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := tryBgpipe("--", "read", in, "--", "write", "--create", out); err == nil {
		t.Fatal("--create: want error for an existing file")
	}
	if got := read(out); got != "existing\n" {
		t.Fatalf("--create: existing file changed to %q", got)
	}

	// --create writes a new file
	out = filepath.Join(dir, "new.json")
	runBgpipe(t, "--", "read", in, "--", "write", "--create", out)
	if got := read(out); got != msg {
		t.Fatalf("--create: got %q, want %q", got, msg)
	}

	// a *.tmp target is the user's file: appended to, never renamed
	out = filepath.Join(dir, "x.json.tmp")
	runBgpipe(t, "--", "read", in, "--", "write", "--format", "json", "--append", out)
	if got := read(out); got != msg {
		t.Fatalf("*.tmp target: got %q, want %q", got, msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.json")); err == nil {
		t.Fatal("*.tmp target: renamed to x.json")
	}
}
