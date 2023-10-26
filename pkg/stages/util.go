package stages

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
	"github.com/spf13/pflag"
)

func tcp_handle(s *bgpipe.StageBase, conn net.Conn, in *pipe.Input) error {
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

	// read from conn -> write to s.Input
	go func() {
		n, err := io.Copy(in, conn)
		s.Trace().Err(err).Msg("connection reader returned")
		tcp.CloseRead()
		rch <- retval{n, err}
	}()

	// write to conn <- read from s.Output
	go func() {
		n, err := tcp.ReadFrom(s.Downstream)
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

func FnFlag(name, short, usage string, fn func()) *pflag.Flag {
	v := fnValue(fn)
	return &pflag.Flag{
		Name:        name,
		Shorthand:   short,
		Usage:       usage,
		Value:       &v,
		NoOptDefVal: "true",
	}
}

type fnValue func()

func (fn *fnValue) IsBoolFlag() bool { return true }

func (fn *fnValue) String() string {
	return "fn"
}

func (fn *fnValue) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	if v {
		(*fn)()
	}
	return nil
}

func (b *fnValue) Type() string {
	return "bool"
}
