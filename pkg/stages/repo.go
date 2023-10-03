package stages

import "github.com/bgpfix/bgpipe/pkg/bgpipe"

var Repo = map[string]bgpipe.NewStage{
	"tcp":     NewTcp,
	"speaker": NewSpeaker,
	"listen":  NewListen,
	"mrt":     NewMrt,
	"stdout":  NewStdout,
	"stdin":   NewStdin,
}
