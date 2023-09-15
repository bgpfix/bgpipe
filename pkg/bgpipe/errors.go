package bgpipe

import "errors"

var (
	ErrStageCmd  = errors.New("invalid stage command")
	ErrStageDiff = errors.New("already defined but different")
)
