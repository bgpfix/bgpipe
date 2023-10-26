package main

import (
	"github.com/bgpfix/bgpipe/bgpipe"
	"github.com/bgpfix/bgpipe/stages"
)

func main() {
	bp := bgpipe.NewBgpipe(
		stages.Repo, // standard stage commands
	)
	bp.Run()
}
