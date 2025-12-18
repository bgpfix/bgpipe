package main

import (
	"os"
	"time"

	bgpipe "github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/stages"
)

// should be set at build time using -ldflags "-X main.BuildVersion=..."
var BuildVersion = "dev"

// use UTC for all time operations
func init() {
	time.Local = time.UTC
}

func main() {
	bp := bgpipe.NewBgpipe(
		BuildVersion,
		stages.Repo, // standard stage commands
	)

	if bp.Run() != nil {
		os.Exit(1)
	}
}
