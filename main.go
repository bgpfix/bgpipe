package main

import (
	bgpipe "github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/stages"
)

func main() {
	bp := bgpipe.NewBgpipe(
		stages.Repo, // standard stage commands
	)
	bp.Run()
}
