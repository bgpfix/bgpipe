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

// ReadCredential resolves v as $ENV_VAR, a literal user:pass (has a colon),
// or else a file path. The result must be in user:pass format.
func ReadCredential(v string) ([]byte, error) {
	var cred []byte
	var err error
	switch {
	case len(v) > 1 && v[0] == '$':
		cred = []byte(os.Getenv(v[1:]))
	case strings.Contains(v, ":"):
		cred = []byte(v)
	default:
		cred, err = os.ReadFile(v)
		if err != nil {
			return nil, err
		}
		cred, _, _ = bytes.Cut(cred, []byte{'\n'})
	}
	if cred = bytes.TrimSpace(cred); !bytes.Contains(cred, []byte{':'}) {
		return nil, fmt.Errorf("%s: need user:pass", v)
	}
	return cred, nil
}
