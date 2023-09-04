package bgpipe

import (
	"github.com/bgpfix/bgpfix/pipe"
)

type Step interface {
	// Init initializes Step at position pos, parsing given CLI args.
	Init(pos int, args []string) error

	// Attach attaches to given bgpfix pipe p.
	Attach(p *pipe.Pipe) error
}
