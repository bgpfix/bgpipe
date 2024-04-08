package stages

import "github.com/bgpfix/bgpipe/core"

var Repo = map[string]core.NewStage{
	"connect":   NewConnect,
	"exec":      NewExec,
	"limit":     NewLimit,
	"listen":    NewListen,
	"mrt":       NewMrt,
	"pipe":      NewPipe,
	"read":      NewRead,
	"speaker":   NewSpeaker,
	"stdin":     NewStdin,
	"stdout":    NewStdout,
	"websocket": NewWebsocket,
	"write":     NewWrite,
}
