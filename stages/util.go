package stages

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

func tcp_handle(s *core.StageBase, conn net.Conn, in *pipe.Input, timeout time.Duration) error {
	s.Info().Msgf("connected %s -> %s", conn.LocalAddr(), conn.RemoteAddr())
	defer conn.Close()

	// get tcp conn
	tcp, _ := conn.(*net.TCPConn)
	if tcp == nil {
		return fmt.Errorf("could not get TCPConn")
	}

	// discard data after conn.Close()
	if err := tcp.SetLinger(0); err != nil {
		s.Info().Err(err).Msg("SetLinger failed")
	}

	// variables for reader / writer
	type retval struct {
		n   int64
		err error
	}
	rch := make(chan retval, 1)
	wch := make(chan retval, 1)

	// read from conn
	go func() {
		n, err := io.Copy(in, conn)
		s.Trace().Err(err).Msg("connection reader returned")
		tcp.CloseRead()
		rch <- retval{n, err}

		if timeout > 0 {
			time.Sleep(timeout)
			s.Cancel(io.EOF)
		}
	}()

	// write to conn
	go func() {
		pipeline := s.P.LineFor(s.Dir.Flip())
		n, err := tcp.ReadFrom(pipeline)
		s.Trace().Err(err).Msg("connection writer returned")
		tcp.CloseWrite()
		wch <- retval{n, err}

		if timeout > 0 {
			time.Sleep(timeout)
			s.Cancel(io.EOF)
		}
	}()

	// wait for error on any side, or both sides EOF
	var read, wrote int64
	var err error
	running := 2
	for err == nil && running > 0 {
		select {
		case <-s.Ctx.Done():
			err = context.Cause(s.Ctx)
			running = 0
		case r := <-rch:
			read, err = r.n, r.err
			running--
			if err == io.EOF {
				err = nil
			}
		case w := <-wch:
			wrote, err = w.n, w.err
			running--
			if err == io.EOF {
				err = nil
			}
		}
	}

	s.Info().Err(err).Int64("read", read).Int64("wrote", wrote).Msg("connection closed")
	return err
}

func close_safe[T any](ch chan T) (ok bool) {
	if ch != nil {
		defer func() { recover() }()
		close(ch)
		return true
	}
	return
}

func send_safe[T any](ch chan T, v T) (ok bool) {
	if ch != nil {
		defer func() { recover() }()
		ch <- v
		return true
	}
	return
}
