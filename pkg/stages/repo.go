package stages

import "github.com/bgpfix/bgpipe/pkg/bgpipe"

var Repo = map[string]bgpipe.NewStage{
	"tcp":     NewTcpConnect,
	"speaker": NewSpeaker,
	"mrt":     NewMrt,
	"stdout":  NewStdout,
	"stdin":   NewStdin,
}
