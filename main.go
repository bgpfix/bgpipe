package main

import (
	"os"

	bgpipe "github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/stages"
)

// should be set at build time using -ldflags "-X main.BuildVersion=..."
var BuildVersion = "dev"

func main() {
	bp := bgpipe.NewBgpipe(
		BuildVersion,
		stages.Repo, // standard stage commands
	)

	if bp.Run() != nil {
		os.Exit(1)
	}
}
