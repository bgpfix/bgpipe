package stages

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// the last JSON line is parsed even if it has no newline
func TestReadLastLineWithoutNewline(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.json"), filepath.Join(dir, "out.json")
	line := `["R",1,"2026-01-01T00:00:00.000","KEEPALIVE",null,null]`
	if err := os.WriteFile(in, []byte(line+"\n"+line), 0644); err != nil {
		t.Fatal(err)
	}

	runBgpipe(t, "--", "read", in, "--", "write", out)

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(got), "KEEPALIVE"); n != 2 {
		t.Errorf("got %d messages, want 2: %s", n, got)
	}
}

// a text feed without newlines must not be buffered without bound
func TestReadEndlessLine(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	if err := os.WriteFile(in, bytes.Repeat([]byte("a"), 9<<20), 0644); err != nil {
		t.Fatal(err)
	}

	if err := tryBgpipe("--", "read", in, "--", "write", filepath.Join(dir, "out.json")); err == nil {
		t.Error("want an error for a 9 MiB line")
	}
}
