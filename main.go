package main

import (
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
	"github.com/bgpfix/bgpipe/pkg/stages"
)

func main() {
	bp := bgpipe.NewBgpipe(
		stages.Repo, // standard stage commands
	)
	bp.Run()
}
