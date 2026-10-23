package core

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// ReadSecret resolves v as $ENV_VAR, a file path (starting with / or ./),
// or a literal value. Files are read up to the first newline.
func ReadSecret(v string) ([]byte, error) {
	var val []byte
	switch {
	case len(v) > 1 && v[0] == '$':
		val = []byte(os.Getenv(v[1:]))
	case strings.HasPrefix(v, "/") || strings.HasPrefix(v, "./"):
		buf, err := os.ReadFile(v)
		if err != nil {
			return nil, err
		}
		val, _, _ = bytes.Cut(buf, []byte{'\n'})
	default:
		val = []byte(v)
	}
	if val = bytes.TrimSpace(val); len(val) == 0 {
		return nil, fmt.Errorf("%s: empty value", v)
	}
	return val, nil
}
