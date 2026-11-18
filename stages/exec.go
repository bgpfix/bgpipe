package stages

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/extio"
)

type Exec struct {
	*core.StageBase

	cmd_path string
	cmd_args []string
	cmd_exec *exec.Cmd
	cmd_in   io.WriteCloser // stdin
	cmd_out  io.ReadCloser  // stdout
	cmd_err  io.ReadCloser  // stderr

	eio *extio.Extio
}

func NewExec(parent *core.StageBase) core.Stage {
	var (
		s = &Exec{StageBase: parent}
		o = &s.Options
	)

	o.Usage = "exec COMMAND | exec -A COMMAND [COMMAND-ARGUMENTS...] --"
	o.Args = []string{"cmd"}
	o.Descr = "handle messages in a background process"
	o.IsProducer = true
	o.FilterIn = true
	o.FilterOut = true
	o.Bidir = true
	o.StopUpstream = true

	s.eio = extio.NewExtio(parent, 0, false)
	s.eio.ExaAck = true
	return s
}

func (s *Exec) Attach() error {
	// check command
	k := s.K
	s.cmd_path = k.String("cmd")
	s.cmd_args = k.Strings("args")
	if len(s.cmd_path) == 0 {
		return errors.New("needs path to the executable")
	}

	// create cmd
	var err error
	s.cmd_exec = exec.CommandContext(s.Ctx, s.cmd_path, s.cmd_args...)
	s.cmd_in, err = s.cmd_exec.StdinPipe()
	if err != nil {
		return err
	}
	s.cmd_out, err = s.cmd_exec.StdoutPipe()
	if err != nil {
		return err
	}
	s.cmd_err, err = s.cmd_exec.StderrPipe()
	if err != nil {
		return err
	}

	// cleanup procedure
	// s.cmd_exec.Cancel = func() error { util.Close(s.eio.Output); return nil }
	s.cmd_exec.WaitDelay = time.Second

	return s.eio.Attach()
}

// Prepare starts the command in background
func (s *Exec) Prepare() (err error) {
	s.Info().Msgf("running %s", s.cmd_exec.String())
	return s.cmd_exec.Start()
}

// Stop closes the command stdin: it should finish, and its output is still read
func (s *Exec) Stop() error {
	s.eio.OutputClose()
	return nil
}

// Run feeds the command and reads its output, until it exits
func (s *Exec) Run() error {
	go s.stderrReader()
	go func() {
		s.eio.WriteStream(s.cmd_in) // NB: until Stop, or the command stops reading
		s.cmd_in.Close()
	}()

	rerr := s.eio.ReadStream(s.cmd_out, nil)
	if rerr != nil {
		s.cmd_exec.Process.Kill() // NB: it may block writing to us
	}
	err := s.cmd_exec.Wait() // NB: after all output is read
	s.Err(err).Msg("command terminated")
	if s.Ctx.Err() != nil {
		return nil // stopped
	} else if rerr != nil {
		return rerr
	}
	return err
}

func (s *Exec) stderrReader() {
	// NB: log long lines in chunks, never stop reading (would block the child)
	in := bufio.NewReaderSize(s.cmd_err, 64*1024)
	for {
		line, err := in.ReadSlice('\n')
		if len(line) > 0 {
			s.Info().Msg(string(bytes.TrimRight(line, "\r\n")))
		}
		if err != nil && err != bufio.ErrBufferFull {
			return
		}
	}
}
