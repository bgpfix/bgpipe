package stages

import "github.com/bgpfix/bgpipe/core"

var Repo = map[string]core.NewStage{
	"connect":   NewConnect,
	"drop":      NewGrep,
	"exec":      NewExec,
	"grep":      NewGrep,
	"limit":     NewLimit,
	"listen":    NewListen,
	"pipe":      NewPipe,
	"read":      NewRead,
	"ris-live":  NewRisLive,
	"speaker":   NewSpeaker,
	"stdin":     NewStdin,
	"stdout":    NewStdout,
	"tag":       NewTag,
	"update":    NewUpdate,
	"websocket": NewWebsocket,
	"write":     NewWrite,
}
