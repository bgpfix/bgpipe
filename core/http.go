package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (b *Bgpipe) configureHTTP() error {
	addr := strings.TrimSpace(b.K.String("http"))
	if addr == "" {
		b.HTTP = nil
		b.httpmux = nil
		return nil
	}

	m := chi.NewRouter()
	b.httpmux = m
	b.HTTP = &http.Server{
		Addr:              addr,
		Handler:           m,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return nil
}

func (b *Bgpipe) startHTTP() error {
	if b.HTTP == nil {
		return nil
	}

	ln, err := net.Listen("tcp", b.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("could not bind --http %s: %w", b.HTTP.Addr, err)
	}

	go func() {
		err := b.HTTP.Serve(ln)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return
		}
		b.Cancel(fmt.Errorf("http server failed: %w", err))
	}()

	b.Info().Str("addr", ln.Addr().String()).Msg("HTTP API listening")
	return nil
}

func (b *Bgpipe) stopHTTP() {
	if b.HTTP == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := b.HTTP.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		b.Warn().Err(err).Msg("HTTP API shutdown error")
	}

}

func (b *Bgpipe) attachHTTPStages() error {
	if b.httpmux == nil {
		return nil
	}

	used := make(map[string]struct{})
	for _, s := range b.Stages {
		if s == nil {
			continue
		}

		r := chi.NewRouter()
		if err := s.Stage.RouteHTTP(r); err != nil {
			return s.Errorf("could not register HTTP API: %w", err)
		}
		if len(r.Routes()) == 0 {
			continue
		}

		base := stageHTTPPath(s)
		if _, exists := used[base]; exists {
			base = fmt.Sprintf("%s-%d", base, s.Index)
		}
		used[base] = struct{}{}

		s.HTTPPath = "/" + base
		b.httpmux.Mount(s.HTTPPath, r)

		s.Info().Str("http", s.HTTPPath).Msg("stage HTTP API mounted")
	}

	return nil
}

func stageHTTPPath(s *StageBase) string {
	name := strings.TrimPrefix(strings.TrimSpace(s.Name), "@")
	if name == "" {
		name = s.Cmd
	}

	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, name)

	name = strings.Trim(name, "-_.")
	if name == "" {
		return fmt.Sprintf("stage-%d", s.Index)
	}

	return name
}
