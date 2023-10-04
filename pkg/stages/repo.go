package stages

import "github.com/bgpfix/bgpipe/pkg/bgpipe"

var Repo = map[string]bgpipe.NewStage{
	"connect": NewConnect,
	"listen":  NewListen,
	"speaker": NewSpeaker,
	"mrt":     NewMrt,
	"stdout":  NewStdout,
	"stdin":   NewStdin,
}
