package stages

import (
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

// TODO
type Exec struct {
	*bgpipe.StageBase

	path string
	cmd  *exec.Cmd
}

func NewExec(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Exec{StageBase: parent}
	o := &s.Options

	o.Args = []string{"path"}

	o.Descr = "pass through a background JSON processor"
	o.IsProducer = true
	o.AllowLR = true

	return s
}

func (s *Exec) Attach() error {
	s.path = s.K.String("path")
	if len(s.path) == 0 {
		return errors.New("needs path to the executable")
	}

	_, err := os.Stat(s.path)
	if err != nil {
		return err
	}

	s.cmd = exec.CommandContext(s.Ctx, s.path)
	s.cmd.WaitDelay = time.Second

	// TODO: attach to pipe OnMsg, write to cmd stdin

	return nil
}

func (s *Exec) Prepare() error {
	return s.cmd.Start()
}

func (s *Exec) Run() error {
	// TODO: read from cmd stdout
	<-s.Ctx.Done()
	return s.cmd.Wait()
}
