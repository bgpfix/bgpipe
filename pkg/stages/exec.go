package stages

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
)

type Exec struct {
	*bgpipe.StageBase

	cmd_path string
	cmd_args []string
	own      bool
	copy     bool

	cmd     *exec.Cmd
	cmd_in  io.WriteCloser
	cmd_out io.ReadCloser
	cmd_err io.ReadCloser

	pool   sync.Pool
	output chan []byte
}

const (
	EXEC_OUTPUT_BUF = 100
)

func NewExec(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s = &Exec{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	o.Args = []string{"cmd"}
	o.Usage = "exec COMMAND | exec -c COMMAND [ARGUMENTS...] --"
	f.Bool("own", false, "do not skip own messages")
	f.Bool("copy", false, "copy messages to command (instead of moving)")
	f.AddFlag(FnFlag("args", "c", "read a list of command arguments until a double-dash marker",
		func() {
			o.Args = make([]string, 0)
		}))

	o.Descr = "pass through a background JSON processor"
	o.IsProducer = true
	o.AllowLR = true

	s.output = make(chan []byte, EXEC_OUTPUT_BUF)

	return s
}

func (s *Exec) Attach() error {
	k := s.K

	// misc options
	s.own = k.Bool("own")
	s.copy = k.Bool("copy")

	// check command
	if args := k.Strings("args"); len(args) > 0 {
		s.cmd_path = args[0]
		s.cmd_args = args[1:]
	} else {
		s.cmd_path = k.String("cmd")
	}
	if len(s.cmd_path) == 0 {
		return errors.New("needs path to the executable")
	}

	// create cmd
	s.cmd = exec.CommandContext(s.Ctx, s.cmd_path, s.cmd_args...)
	s.cmd.WaitDelay = time.Second

	// get I/O pipes
	var err error
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
	s.Info().Msgf("running %s", s.cmd.String())
	return s.cmd.Start()
}

func (s *Exec) Run() (err error) {
	// start stdout reader
	ch_stdout := make(chan error, 1)
	go s.stdoutReader(ch_stdout)

	// start stderr reader
	ch_stderr := make(chan error, 1)
	go s.stderrReader(ch_stderr)

	// start stdin writer
	ch_stdin := make(chan error, 1)
	go s.stdinWriter(ch_stdin)

	// cleanup on exit
	defer func() {
		// possibly unblock [1]
		if ch_stdin != nil {
			close(s.output)
		}

		// kill the command and wait for it to exit
		s.cmd.Cancel()
		cmd_err := s.cmd.Wait()
		s.Err(cmd_err).Msg("command terminated")

		// escalate the error?
		if cmd_err != nil {
			if err == nil {
				err = cmd_err
			}
		}
	}()

	// wait
	for {
		select {
		case err := <-ch_stdout:
			s.Debug().Err(err).Msg("stdout reader done")
			if err == nil {
				err = io.EOF
			}
			return fmt.Errorf("stdout closed: %w", err)
		case err := <-ch_stdin:
			s.Debug().Err(err).Msg("stdin writer done")
			close(s.output)
			ch_stdin = nil // continue, ignore stderr
		case err := <-ch_stderr:
			s.Debug().Err(err).Msg("stderr reader done")
			ch_stderr = nil // continue, ignore stderr
		case <-s.Ctx.Done():
			return context.Cause(s.Ctx)
		}
	}
}

func (s *Exec) stdoutReader(done chan error) {
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
		// s.Trace().Msgf("out: %s", buf)

		// detect the format
		var err error
		switch {
		case buf[0] == '[': // full message
			// TODO: can re-use recent message?
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
		if m.Dst == msg.DST_L {
			p.L.ProcessMsg(m)
		} else {
			p.R.ProcessMsg(m)
		}
	}
	done <- in.Err()
}

func (s *Exec) stderrReader(done chan error) {
	defer close(done)

	in := bufio.NewScanner(s.cmd_err)
	for in.Scan() {
		s.Info().Msg(in.Text())
	}
	done <- in.Err()
}

func (s *Exec) stdinWriter(done chan error) {
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
	pc := pipe.Context(m)

	// skip our messages?
	if pc.SourceId == s.Index && !s.own {
		return
	}

	// drop the message?
	if !s.copy {
		// TODO: add borrow if not set already, and keep for later re-use
		pc.Action.Add(pipe.ACTION_DROP)
	}

	// get from pool, marshal
	buf, _ := s.pool.Get().([]byte)
	buf = m.ToJSON(buf[:0])
	buf = append(buf, '\n')

	// try writing, don't panic on channel closed [1]
	defer func() { recover() }()
	s.output <- buf

	// output full?
	// if len(s.output) == EXEC_OUTPUT_BUF {
	// 	s.Warn().Msg("output buffer full")
	// }

	return
}
