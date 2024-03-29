package stages

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

func tcp_handle(s *core.StageBase, conn net.Conn, in *pipe.Proc) error {
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
	}()

	// write to conn
	go func() {
		pipeline := s.P.LineFor(s.Dir.Flip())
		n, err := tcp.ReadFrom(pipeline)
		s.Trace().Err(err).Msg("connection writer returned")
		tcp.CloseWrite()
		wch <- retval{n, err}
	}()

	// wait for error on any side, or both sides EOF
	var read, wrote int64
	running := 2
	for running > 0 {
		select {
		case <-s.Ctx.Done():
			return context.Cause(s.Ctx)
		case r := <-rch:
			read = r.n
			running--
			if r.err != nil && r.err != io.EOF {
				return r.err
			}
		case w := <-wch:
			wrote = w.n
			running--
			if w.err != nil && w.err != io.EOF {
				return w.err
			}
		}
	}

	s.Info().Int64("read", read).Int64("wrote", wrote).Msg("connection closed")
	return nil
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
