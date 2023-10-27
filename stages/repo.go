package stages

import bgpipe "github.com/bgpfix/bgpipe/core"

var Repo = map[string]bgpipe.NewStage{
	"connect": NewConnect,
	"listen":  NewListen,
	"speaker": NewSpeaker,
	"mrt":     NewMrt,
	"stdout":  NewStdout,
	"stdin":   NewStdin,
	"exec":    NewExec,
	"limit":   NewLimit,
}
