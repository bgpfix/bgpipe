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
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	bgpipe "github.com/bgpfix/bgpipe/core"
	"github.com/valyala/bytebufferpool"
)

type Exec struct {
	*bgpipe.StageBase
	inL *pipe.Proc
	inR *pipe.Proc

	cmd_path string
	cmd_args []string
	own      bool
	copy     bool

	cmd     *exec.Cmd
	cmd_in  io.WriteCloser
	cmd_out io.ReadCloser
	cmd_err io.ReadCloser

	pool   bytebufferpool.Pool             // mem re-use
	output chan *bytebufferpool.ByteBuffer // our output to cmd
}

func NewExec(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s = &Exec{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	o.Usage = "exec COMMAND | exec -A COMMAND [COMMAND-ARGUMENTS...] --"
	o.Descr = "filter messages through a background JSON processor"
	o.IsProducer = true
	o.Bidir = true

	f.Bool("own", false, "do not skip own messages")
	f.Bool("copy", false, "copy messages to command (instead of moving)")
	o.Args = []string{"cmd"}

	return s
}

func (s *Exec) Attach() error {
	// misc options
	k := s.K
	s.own = k.Bool("own")
	s.copy = k.Bool("copy")

	// check command
	s.cmd_path = k.String("cmd")
	s.cmd_args = k.Strings("args")
	if len(s.cmd_path) == 0 {
		return errors.New("needs path to the executable")
	}

	// create cmd
	s.cmd = exec.CommandContext(s.Ctx, s.cmd_path, s.cmd_args...)

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

	// cleanup procedure
	s.cmd.Cancel = func() error { close_safe(s.output); return nil }
	s.cmd.WaitDelay = time.Second

	// attach to pipe
	s.P.OnMsg(s.onMsg, s.Dir)
	s.inL = s.P.AddProc(msg.DIR_L)
	s.inR = s.P.AddProc(msg.DIR_R)

	s.output = make(chan *bytebufferpool.ByteBuffer, 100)
	return nil
}

// Prepare starts the command in background
func (s *Exec) Prepare() (err error) {
	s.Info().Msgf("running %s", s.cmd.String())
	return s.cmd.Start()
}

func (s *Exec) Run() (err error) {
	// start stdout reader
	stdout_reader_done := make(chan error, 1)
	go s.stdoutReader(stdout_reader_done)

	// start stderr reader
	stderr_reader_done := make(chan error, 1)
	go s.stderrReader(stderr_reader_done)

	// start stdin writer
	stdin_writer_done := make(chan error, 1)
	go s.stdinWriter(stdin_writer_done)

	// cleanup on exit
	defer func() {
		// wait for the command
		s.cmd.Cancel()
		cmd_err := s.cmd.Wait()
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
				err = io.EOF
			}
			return fmt.Errorf("stdout closed: %w", err)
		case err := <-stdin_writer_done:
			s.Debug().Err(err).Msg("stdin writer done")
			stdin_writer_done = nil // continue, ignore stdin
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

func (s *Exec) Stop() error {
	close_safe(s.output)
	return nil
}

func (s *Exec) stdoutReader(done chan error) {
	var (
		p   = s.P
		in  = bufio.NewScanner(s.cmd_out)
		def = msg.DIR_R
	)

	defer close(done)

	for in.Scan() {
		// trim
		buf := bytes.TrimSpace(in.Bytes())
		// s.Trace().Msgf("out: %s", buf)

		// parse into m
		m := p.Get()
		var err error
		switch {
		case len(buf) == 0 || buf[0] == '#':
			continue

		case buf[0] == '[': // full message
			// TODO: can re-use recent message?
			err = m.FromJSON(buf)

		case buf[0] >= 'A' && buf[0] <= 'Z':
			s.Event(string(buf))

		default:
			err = errors.New("invalid input")
		}

		if err != nil {
			s.Error().Err(err).Bytes("buf", buf).Msg("parse error")
			p.Put(m)
			continue
		}

		// fix type?
		if m.Type == msg.INVALID {
			m.Up(msg.KEEPALIVE)
		}

		// fix direction?
		if s.Dir != 0 {
			m.Dir = s.Dir
		} else if m.Dir == 0 {
			m.Dir = def
		}

		// sail
		if m.Dir == msg.DIR_L {
			s.inL.WriteMsg(m)
		} else {
			s.inR.WriteMsg(m)
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
	defer func() {
		close_safe(s.output)
		s.cmd_in.Close()
		close(done)
	}()

	out := s.cmd_in
	fh, _ := out.(*os.File)

	for bb := range s.output {
		_, err := bb.WriteTo(out)
		s.pool.Put(bb)

		// success?
		if err != nil {
			done <- err
			return
		}

		// TODO: has any effect?
		if fh != nil {
			fh.Sync()
		}
	}
}

func (s *Exec) onMsg(m *msg.Msg) (action pipe.Action) {
	mx := pipe.MsgContext(m)

	// skip our messages?
	if mx.Input.Id == s.Index && !s.own {
		return
	}

	// drop the message?
	if !s.copy {
		// TODO: if enabled, add borrow if not set already, and keep for later re-use
		mx.Action.Add(pipe.ACTION_DROP)
	}

	// get from pool, marshal
	bb := s.pool.Get()
	bb.B = m.ToJSON(bb.B)
	bb.WriteByte('\n')

	// try writing, don't panic on channel closed [1]
	// TODO: optimize and avoid JSON serialization on next stage (if needed again)?
	if !send_safe(s.output, bb) {
		return
	}

	// output full?
	// if len(s.output) == EXEC_OUTPUT_BUF {
	// 	s.Warn().Msg("output buffer full")
	// }

	return
}
