package stages

import "github.com/bgpfix/bgpipe/core"

var Repo = map[string]core.NewStage{
	"connect":   NewConnect,
	"listen":    NewListen,
	"speaker":   NewSpeaker,
	"mrt":       NewMrt,
	"stdout":    NewStdout,
	"stdin":     NewStdin,
	"exec":      NewExec,
	"limit":     NewLimit,
	"websocket": NewWebsocket,
}
