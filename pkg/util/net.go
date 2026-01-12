package util

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/netip"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

func ConnPublish(s *core.StageBase, conn net.Conn) {
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
		s.Error().Msg("ConnPublish: not first or last stage")
		return
	}

	kv := s.P.KV
	for name, val := range todo {
		addrport, err := netip.ParseAddrPort(val)
		if err != nil {
			s.Err(err).Msgf("ConnPublish %s: could not parse %s", name, val)
			continue
		}
		s.Info().Msgf("connection %s = %s", name, addrport.String())
		kv.Store(name, addrport)
		kv.Store(name+"_ADDR", addrport.Addr())
		kv.Store(name+"_PORT", addrport.Port())
	}
}

func ConnHandle(s *core.StageBase, conn net.Conn, in *pipe.Input, timeout time.Duration) error {
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

// DialRetry is a dialer.DialContext wrapper that adds connection timeout and retry with exponential backoff and jitter.
// stage s can have konfig options: "retry" (bool), "retry-max" (int), "timeout" (duration), and "insecure" (bool).
func DialRetry(s *core.StageBase, dialer *net.Dialer, network, address string, do_tls bool) (net.Conn, error) {
	k := s.K
	retry := k.Bool("retry")
	retry_max := k.Int("retry-max")
	timeout := k.Duration("timeout")
	insecure := k.Bool("insecure")

	if dialer == nil {
		dialer = &net.Dialer{}
	}

	var ctx context.Context
	var cancel context.CancelFunc

	for try := 0; ; try++ {
		// need to wait before retrying?
		if try > 0 {
			sec := min(60, try*try) + rand.Intn(try)
			s.Info().Msgf("dialing %s %s (retry %d in %ds)", network, address, try, sec)

			select {
			case <-time.After(time.Second * time.Duration(sec)):
				break
			case <-s.Ctx.Done():
				return nil, context.Cause(s.Ctx)
			}
		} else { // first attempt
			s.Info().Msgf("dialing %s %s", network, address)
		}

		// add timeout?
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(s.Ctx, timeout)
		} else {
			ctx, cancel = s.Ctx, nil
		}

		// attempt the dial
		var conn net.Conn
		var err error
		if do_tls {
			tls_dialer := &tls.Dialer{
				NetDialer: dialer,
				Config: &tls.Config{
					InsecureSkipVerify: insecure,
				},
			}
			conn, err = tls_dialer.DialContext(ctx, network, address)
		} else {
			conn, err = dialer.DialContext(ctx, network, address)
		}
		if cancel != nil {
			cancel()
		}

		// check the result
		if err == nil {
			return conn, nil // success
		} else if !retry || (retry_max > 0 && try >= retry_max) {
			return nil, err // no (more) retries
		} else if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			continue // temporary timeout, retry
		} else if timeout > 0 && errors.Is(err, context.DeadlineExceeded) {
			continue // context timeout, retry
		} else {
			return nil, err // non-retryable error
		}
	}
}
