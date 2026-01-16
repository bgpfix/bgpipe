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

// ConnBGP handles an opened BGP connection conn for stage s
func ConnBGP(s *core.StageBase, conn net.Conn, in *pipe.Input) error {
	closed_timeout := s.K.Duration("closed-timeout")

	s.Info().Msgf("connected %s -> %s", conn.LocalAddr(), conn.RemoteAddr())
	defer conn.Close()

	// get tcp conn
	tcp, _ := conn.(*net.TCPConn)
	if tcp == nil {
		return fmt.Errorf("could not get TCPConn")
	}

	// discard data after conn.Close()?
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

		if closed_timeout > 0 {
			time.Sleep(closed_timeout)
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

		if closed_timeout > 0 {
			time.Sleep(closed_timeout)
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

// DialRetry is a dialer.DialContext wrapper that adds connection timeout and retry with
// exponential backoff and jitter. Stage s can have many konfig options to tune the dialing.
func DialRetry(s *core.StageBase, dialer *net.Dialer, network, address string) (net.Conn, error) {
	k := s.K
	timeout := k.Duration("timeout")
	retry := k.Bool("retry")
	retry_max := k.Int("retry-max")
	do_tls := k.Bool("tls")
	insecure := k.Bool("insecure")

	// make a copy of the dialer (in case we need to modify it)
	var dial net.Dialer
	if dialer != nil {
		dial = *dialer
	}

	// tune keepalive?
	if v := k.Duration("keepalive"); v != 0 {
		dial.KeepAlive = v
	}

	// bind to given local address?
	if v := k.String("bind"); v != "" {
		// bind needs a port number?
		if _, _, err := net.SplitHostPort(v); err != nil {
			if a, err := netip.ParseAddr(v); err == nil {
				v = netip.AddrPortFrom(a, 0).String()
			} else {
				v += ":0" // no idea, best-effort try
			}
		}

		// resolve and set local address
		la, err := net.ResolveTCPAddr(network, v)
		if err != nil {
			return nil, fmt.Errorf("could not resolve local address %s: %w", v, err)
		}
		dial.LocalAddr = la
	}

	// disable IPv6?
	if k.Bool("no-ipv6") {
		dial.FallbackDelay = -1
		dial.DualStack = false
	}

	// dial timeout
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout == 0 {
		timeout = 15 * time.Second // default timeout
	}

	for try := 0; ; try++ {
		// need to wait before retrying?
		if try > 0 {
			sec := min(60, try*try) + rand.Intn(try)
			s.Info().Msgf("dialing %s %s (retry %d/%d in %ds)", network, address, try, retry_max, sec)

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
				NetDialer: &dial,
				Config: &tls.Config{
					InsecureSkipVerify: insecure,
				},
			}
			conn, err = tls_dialer.DialContext(ctx, network, address)
		} else {
			conn, err = dial.DialContext(ctx, network, address)
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
			s.Warn().Err(err).Msg("dial timeout, retrying")
			continue // temporary timeout, retry
		} else if timeout > 0 && errors.Is(err, context.DeadlineExceeded) {
			s.Warn().Err(err).Msg("dial timeout, retrying")
			continue // context timeout, retry
		} else {
			return nil, err // non-retryable error
		}
	}
}
