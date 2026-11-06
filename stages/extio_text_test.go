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

// with --pardon an oversized line is skipped entirely, and the next one is read
func TestReadOversizedLinePardon(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.json"), filepath.Join(dir, "out.json")
	line := `["R",1,"2026-01-01T00:00:00.000","KEEPALIVE",null,null]` + "\n"

	// '#' comment lines hide a valid message after the 64 KiB read boundary
	big := "#" + strings.Repeat("a", 9<<20) + line
	if err := os.WriteFile(in, []byte(big+line), 0644); err != nil {
		t.Fatal(err)
	}
	x := filepath.Join(dir, "x.json")
	if err := tryBgpipe("--", "read", in, "--", "write", x); err == nil {
		t.Error("want an error for a 9 MiB line")
	}
	if got, _ := os.ReadFile(x); strings.Contains(string(got), "KEEPALIVE") {
		t.Errorf("messages after an oversized line got through: %.200s", got)
	}

	runBgpipe(t, "--", "read", "--pardon", in, "--", "write", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(got), "KEEPALIVE"); n != 1 {
		t.Errorf("got %d messages, want 1: %.200s", n, got)
	}
}
