package stages

import "github.com/bgpfix/bgpipe/bgpipe"

var Repo = map[string]bgpipe.NewStage{
	"connect": NewConnect,
	"listen":  NewListen,
	"speaker": NewSpeaker,
	"mrt":     NewMrt,
	"stdout":  NewStdout,
	"stdin":   NewStdin,
	"exec":    NewExec,
}
