package extio

import "errors"

var (
	ErrFormat = errors.New("unrecognized format")
	ErrLength = errors.New("invalid buffer length")
)
