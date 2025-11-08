package stages

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

func conn_publish(s *core.StageBase, conn net.Conn) {
	var todo map[string]string
	if s.IsFirst {
		todo = map[string]string{
			"L_LOCAL":  conn.LocalAddr().String(),
			"L_REMOTE": conn.RemoteAddr().String(),
		}
	} else if s.IsLast {
		todo = map[string]string{
			"R_LOCAL":  conn.LocalAddr().String(),
			"R_REMOTE": conn.RemoteAddr().String(),
		}
	} else {
		s.Error().Msg("conn_publish: not first or last stage")
		return
	}

	kv := s.P.KV
	for name, val := range todo {
		addrport, err := netip.ParseAddrPort(val)
		if err != nil {
			s.Err(err).Msgf("conn_publish %s: could not parse %s", name, val)
			continue
		}
		s.Info().Msgf("connection %s = %s", name, addrport.String())
		kv.Store(name, addrport)
		kv.Store(name+"_ADDR", addrport.Addr())
		kv.Store(name+"_PORT", addrport.Port())
	}
}

func conn_handle(s *core.StageBase, conn net.Conn, in *pipe.Input, timeout time.Duration) error {
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

// close_safe closes channel ch if ch != nil.
// It recovers from panic if the channel is already closed.
// It returns ok=true if the channel was closed successfully.
func close_safe[T any](ch chan T) (ok bool) {
	if ch == nil {
		return
	}
	defer func() {
		if !ok {
			recover()
		}
	}()
	close(ch)
	return true
}

// send_safe sends value v to channel ch, if ch != nil.
// It recovers from panic if the channel is closed.
// It returns ok=true if the value was sent successfully.
func send_safe[T any](ch chan T, v T) (ok bool) {
	if ch == nil {
		return
	}
	defer func() {
		if !ok {
			recover()
		}
	}()
	ch <- v
	return true
}

}
