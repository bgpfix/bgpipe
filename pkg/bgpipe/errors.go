package bgpipe

import "errors"

var (
	ErrStageCmd  = errors.New("invalid stage command")
	ErrStageDiff = errors.New("already defined but different")

	ErrFirstL      = errors.New("invalid L direction for first stage")
	ErrLastR       = errors.New("invalid R direction for last stage")
	ErrFirstOrLast = errors.New("must be either the first or the last stage")
)
