package main

import (
	"os"

	bgpipe "github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/stages"
)

func main() {
	bp := bgpipe.NewBgpipe(
		stages.Repo, // standard stage commands
	)

	if bp.Run() != nil {
		os.Exit(1)
	}
}
