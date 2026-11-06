package stages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tag --drop with several keys must drop all of them
func TestTagDropMany(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.json"), filepath.Join(dir, "out.json")
	line := `["R",1,"2026-01-01T00:00:00.000","KEEPALIVE",null,{"A":"1","B":"2","C":"3"}]` + "\n"
	if err := os.WriteFile(in, []byte(line), 0644); err != nil {
		t.Fatal(err)
	}

	runBgpipe(t, "--", "read", in, "--", "tag", "--drop", "A,B", "--", "write", out)

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"C":"3"}`; !strings.Contains(string(got), want) || strings.Contains(string(got), `"A"`) || strings.Contains(string(got), `"B"`) {
		t.Errorf("got %s, want tags %s", got, want)
	}
}
