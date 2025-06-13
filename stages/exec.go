package stages

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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

	f := o.Flags
	f.Bool("keep-stdin", false, "keep running if stdin is closed")
	f.Bool("keep-stdout", false, "keep running if stdout is closed")

	s.eio = extio.NewExtio(parent, 0)
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
	// s.cmd_exec.Cancel = func() error { close_safe(s.eio.Output); return nil }
	s.cmd_exec.WaitDelay = time.Second

	return s.eio.Attach()
}

// Prepare starts the command in background
func (s *Exec) Prepare() (err error) {
	s.Info().Msgf("running %s", s.cmd_exec.String())
	return s.cmd_exec.Start()
}

// Stop stops the flow of data
func (s *Exec) Stop() error {
	s.eio.InputClose()
	s.eio.OutputClose()
	return nil
}

// Run runs the data flow
func (s *Exec) Run() (err error) {
	// start stdout reader
	stdout_reader_done := make(chan error, 1)
	stdout_ok := s.K.Bool("keep-stdout")
	go s.stdoutReader(stdout_reader_done)

	// start stderr reader
	stderr_reader_done := make(chan error, 1)
	go s.stderrReader(stderr_reader_done)

	// start stdin writer
	stdin_writer_done := make(chan error, 1)
	stdin_ok := s.K.Bool("keep-stdin")
	go s.stdinWriter(stdin_writer_done)

	// cleanup on exit
	defer func() {
		// wait for the command
		s.cmd_exec.Cancel()
		cmd_err := s.cmd_exec.Wait()
		s.Err(cmd_err).Msg("command terminated")

		// escalate the error?
		if cmd_err != nil && err == nil {
			err = cmd_err
		}
	}()

	// wait
	for {
		select {
		case err := <-stdout_reader_done:
			s.Debug().Err(err).Msg("stdout reader done")
			if err == nil {
				if stdout_ok {
					continue // it's fine
				} else {
					err = io.EOF // it shouldn't end
				}
			}
			return fmt.Errorf("stdout closed: %w", err)
		case err := <-stdin_writer_done:
			s.Debug().Err(err).Msg("stdin writer done")
			if err != nil && stdin_ok {
				stdin_writer_done = nil // continue, ignore stdin
				continue
			}
			return fmt.Errorf("stdin closed: %w", err)
		case err := <-stderr_reader_done:
			s.Debug().Err(err).Msg("stderr reader done")
			stderr_reader_done = nil // continue, ignore stderr
		case <-s.Ctx.Done():
			err := context.Cause(s.Ctx)
			s.Debug().Err(err).Msg("context cancel")
			return err
		}
	}
}

func (s *Exec) stdoutReader(done chan error) {
	done <- s.eio.ReadStream(s.cmd_out, nil)
	close(done)
}

func (s *Exec) stderrReader(done chan error) {
	in := bufio.NewScanner(s.cmd_err)
	for in.Scan() {
		s.Info().Msg(in.Text())
	}
	done <- in.Err()
	close(done)
}

func (s *Exec) stdinWriter(done chan error) {
	done <- s.eio.WriteStream(s.cmd_in)
	s.cmd_in.Close()
	close(done)
}
