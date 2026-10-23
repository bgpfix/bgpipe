package core

import "errors"

var (
	ErrConfig       = errors.New("configuration error") // exit code 2
	ErrStageCmd     = errors.New("invalid stage command")
	ErrStageDiff    = errors.New("already defined but different")
	ErrPipeFinished = errors.New("pipe stopped")
	ErrStageStopped = errors.New("stage stopped")
	ErrFirstOrLast  = errors.New("must be either the first or the last stage")
	ErrInject       = errors.New("invalid value for the --new option")
	ErrLR           = errors.New("select either --left or --right, not both")
	ErrNoCallbacks  = errors.New("stage has no callbacks registered")
	ErrNoInputs     = errors.New("stage has no inputs registered")
)
