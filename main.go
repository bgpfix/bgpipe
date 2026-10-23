package main

import (
	"errors"
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

	// exit codes: 0 ok, 1 runtime error, 2 config error, 3 killed by --kill event
	if err := bp.Run(); errors.Is(err, bgpipe.ErrConfig) {
		os.Exit(2)
	} else if err != nil {
		os.Exit(1)
	}
}
