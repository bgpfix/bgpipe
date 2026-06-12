package stages

import (
	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/stages/rpki"
	rvlive "github.com/bgpfix/bgpipe/stages/rv-live"
)

var Repo = map[string]core.NewStage{
	"aspa":      rpki.NewAspa,
	"connect":   NewConnect,
	"drop":      NewGrep,
	"exec":      NewExec,
	"grep":      NewGrep,
	"head":      NewHead,
	"limit":     NewLimit,
	"listen":    NewListen,
	"metrics":   NewMetrics,
	"pipe":      NewPipe,
	"read":      NewRead,
	"ris-live":  NewRisLive,
	"rov":       rpki.NewRov,
	"rv-live":   rvlive.NewRvLive,
	"speaker":   NewSpeaker,
	"stdin":     NewStdin,
	"stdout":    NewStdout,
	"tag":       NewTag,
	"update":    NewUpdate,
	"websocket": NewWebsocket,
	"write":     NewWrite,
}
