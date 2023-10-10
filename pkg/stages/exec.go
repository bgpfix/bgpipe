package stages

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Exec struct {
	*bgpipe.StageBase

	path    string
	cmd     *exec.Cmd
	cmd_in  io.WriteCloser
	cmd_out io.ReadCloser
	cmd_err io.ReadCloser

	pool   sync.Pool
	output chan []byte
}

const (
	EXEC_OUTPUT_BUF = 10
)

func NewExec(parent *bgpipe.StageBase) bgpipe.Stage {
	s := &Exec{StageBase: parent}
	o := &s.Options

	o.Args = []string{"path"}

	o.Descr = "pass through a background JSON processor"
	o.IsProducer = true
	o.AllowLR = true

	s.output = make(chan []byte, EXEC_OUTPUT_BUF)

	return s
}

func (s *Exec) Attach() error {
	// check exec path
	s.path = s.K.String("path")
	if len(s.path) == 0 {
		return errors.New("needs path to the executable")
	}
	_, err := os.Stat(s.path)
	if err != nil {
		return err
	}

	// create cmd
	s.cmd = exec.CommandContext(s.Ctx, s.path)
	s.cmd.WaitDelay = time.Second

	// get I/O pipes
	s.cmd_in, err = s.cmd.StdinPipe()
	if err != nil {
		return err
	}
	s.cmd_out, err = s.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	s.cmd_err, err = s.cmd.StderrPipe()
	if err != nil {
		return err
	}

	// attach to pipe OnMsg, write to cmd stdin
	s.P.Options.OnMsg(s.onMsg, s.Dst())

	return nil
}

// Prepare starts the command in background
func (s *Exec) Prepare() (err error) {
	s.Debug().Msgf("starting %s", s.cmd.String())
	return s.cmd.Start()
}

func (s *Exec) Run() (err error) {
	// cleanup on exit
	defer func() {
		// stop runStdin, possibly unblock onMsg writers [1]
		close(s.output)

		// cancel our context and wait for the command to exit
		s.Cancel(err)
		cmd_err := s.cmd.Wait()

		// escalate the error?
		if cmd_err != nil {
			s.Error().Err(cmd_err).Msg("command terminated")
			if err == nil {
				err = cmd_err
			}
		} else {
			s.Debug().Msg("command stopped cleanly")
		}
	}()

	// start stdout reader
	ch_stdout := make(chan error, 1)
	go s.runStdout(ch_stdout)

	// start stderr reader
	ch_stderr := make(chan error, 1)
	go s.runStderr(ch_stderr)

	// start stdin writer
	ch_stdin := make(chan error, 1)
	go s.runStdin(ch_stdin)

	// wait
	for {
		select {
		case err := <-ch_stdout:
			if err != nil {
				return fmt.Errorf("stdout: %w", err)
			} else {
				s.Debug().Msg("stdout reader done")
				return nil // clean exit
			}
		case err := <-ch_stderr:
			if err != nil {
				return fmt.Errorf("stderr: %w", err)
			} else {
				s.Debug().Msg("stderr reader done")
				ch_stderr = nil // continue, ignore stderr
			}
		case err := <-ch_stdin:
			if err != nil {
				return fmt.Errorf("stdin: %w", err)
			} else {
				s.Debug().Msg("stdin writer done")
				ch_stdin = nil // continue, ignore stdin
			}
		case <-s.Ctx.Done():
			return context.Cause(s.Ctx)
		}
	}
}

func (s *Exec) runStdout(done chan error) {
	defer close(done)

	var (
		p   = s.P
		in  = bufio.NewScanner(s.cmd_out)
		dst = s.Dst()
		def = msg.DST_R
	)

	for m := s.NewMsg(); in.Scan(); m = s.NewMsg() {
		// trim
		buf := bytes.TrimSpace(in.Bytes())
		s.Trace().Msgf("out: %s", buf)

		// detect the format
		var err error
		switch {
		case buf[0] == '[': // full message
			// TODO: recent message
			err = m.FromJSON(buf)

		case buf[0] >= 'A' && buf[0] <= 'Z':
			s.Event(string(buf), nil)

		default:
			err = errors.New("invalid input")
		}

		if err != nil {
			s.Error().Err(err).Bytes("input", buf).Msg("parse error")
			continue
		}

		// fix type?
		if m.Type == msg.INVALID {
			m.Up(msg.KEEPALIVE)
		}

		// fix direction?
		if dst != 0 {
			m.Dst = dst
		} else if m.Dst == 0 {
			m.Dst = def
		}

		// sail
		// FIXME: --in next
		if m.Dst == msg.DST_L {
			p.L.WriteMsg(m)
		} else {
			p.R.WriteMsg(m)
		}
	}
	done <- in.Err()
}

func (s *Exec) runStderr(done chan error) {
	defer close(done)

	in := bufio.NewScanner(s.cmd_err)
	for in.Scan() {
		s.Info().Msg(in.Text())
	}
	done <- in.Err()
}

func (s *Exec) runStdin(done chan error) {
	defer close(done)

	out := s.cmd_in
	for buf := range s.output {
		_, err := out.Write(buf)
		s.pool.Put(buf)
		if err != nil {
			done <- err
			break
		}
	}
}

func (s *Exec) onMsg(m *msg.Msg) (action pipe.Action) {
	// drop the message, at least for now
	pc := pipe.Context(m)
	pc.Action.Add(pipe.ACTION_DROP) // TODO: if not set, add borrow and keep for later

	// get from pool, marshal
	buf, _ := s.pool.Get().([]byte)
	buf = m.ToJSON(buf[:0])
	buf = append(buf, '\n')

	// output full?
	if len(s.output) == EXEC_OUTPUT_BUF {
		s.Warn().Msg("output buffer full")
	}

	// try writing, don't panic on channel closed [1]
	defer func() { recover() }()
	s.output <- buf
	return
}
