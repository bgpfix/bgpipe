package stages

import (
	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/stages/rpki"
	rvlive "github.com/bgpfix/bgpipe/stages/rv-live"
)

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
	"rpki":      rpki.NewRpki,
	"rv-live":   rvlive.NewRvLive,
	"speaker":   NewSpeaker,
	"stdin":     NewStdin,
	"stdout":    NewStdout,
	"tag":       NewTag,
	"update":    NewUpdate,
	"websocket": NewWebsocket,
	"write":     NewWrite,
}
