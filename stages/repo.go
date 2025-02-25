package stages

import "github.com/bgpfix/bgpipe/core"

var Repo = map[string]core.NewStage{
	"attr":      NewAttr,
	"connect":   NewConnect,
	"exec":      NewExec,
	"grep":      NewGrep,
	"limit":     NewLimit,
	"listen":    NewListen,
	"pipe":      NewPipe,
	"read":      NewRead,
	"speaker":   NewSpeaker,
	"stdin":     NewStdin,
	"stdout":    NewStdout,
	"websocket": NewWebsocket,
	"write":     NewWrite,
}
