package bgpipe

import "errors"

var (
	ErrStageCmd     = errors.New("invalid stage command")
	ErrStageDiff    = errors.New("already defined but different")
	ErrStageStopped = errors.New("stage stopped")
	ErrFirstOrLast  = errors.New("must be either the first or the last stage")
	ErrInject       = errors.New("invalid --in option value")
	ErrLR           = errors.New("select either --left or --right, not both")
	ErrKill         = errors.New("session killed by an event")
)
