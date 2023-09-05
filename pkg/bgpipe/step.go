package bgpipe

import (
	"github.com/bgpfix/bgpfix/pipe"
)

// Step represents one step in a bgpfix pipe
type Step interface {
	// Init initializes step with given CLI args.
	Init(args []string) error

	// Attach attaches to given bgpfix pipe p.
	Attach(p *pipe.Pipe) error
}

// NewStepFunc returns a new Step for step at position pos.
type NewStepFunc func(b *Bgpipe, pos int) Step

// NewStepFuncs maps step commands to corresponding NewStepFunc
var NewStepFuncs = map[string]NewStepFunc{
	"connect": NewTcpConnect,
}
