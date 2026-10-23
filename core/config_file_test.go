package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.conf")
	os.WriteFile(path, []byte(`bgpipe --log debug \
  # comment
connect 192.0.2.1 # the peer
update --add-com 65000:1 \
       --nexthop4 10.0.0.1
grep "prefix ~ 8.0.0.0/8" 'a "b"' c\ d
    -- connect 10.0.0.1
`), 0o600)
	args, err := readConfig(path)
	require.NoError(t, err)
	require.Equal(t, []string{"--log", "debug",
		"--", "connect", "192.0.2.1",
		"--", "update", "--add-com", "65000:1", "--nexthop4", "10.0.0.1",
		"--", "grep", "prefix ~ 8.0.0.0/8", `a "b"`, "c d",
		"--", "connect", "10.0.0.1"}, args)

	b := NewBgpipe("test")
	args, err = b.spliceConfig([]string{"-n", "-l", "debug", "-f", path, "--", "stdout"})
	require.NoError(t, err)
	require.Equal(t, "-n", args[0])
	require.Equal(t, "connect", args[6])
	require.Equal(t, "stdout", args[len(args)-1])

	// -f of a stage (here: awk) must stay as-is
	args, err = b.spliceConfig([]string{"exec", "-A", "awk", "-f", "x.awk"})
	require.NoError(t, err)
	require.Equal(t, []string{"exec", "-A", "awk", "-f", "x.awk"}, args)

	words, err := splitWords(`'' "a \"b\" \\" c`)
	require.NoError(t, err)
	require.Equal(t, []string{"", `a "b" \`, "c"}, words)
}
