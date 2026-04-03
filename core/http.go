package core

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"

	vmmetrics "github.com/VictoriaMetrics/metrics"
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

	// auth middleware (nil when no auth configured)
	mw, err := b.httpAuthMiddleware()
	if err != nil {
		return err
	}
	if mw != nil {
		m.Use(mw)
	}

	// fixed routes
	m.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		vmmetrics.WritePrometheus(w, true)
	})
	m.Get("/hc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": b.Version,
			"stages":  b.StageCount(),
			"uptime":  time.Since(b.StartTime).Truncate(time.Second).String(),
		})
	})
	m.Get("/", b.httpDashboard)

	// pprof
	if err := b.configurePprof(m); err != nil {
		return err
	}

	b.httpmux = m
	b.HTTP = &http.Server{
		Addr:              addr,
		Handler:           m,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return nil
}

// httpAuthMiddleware returns middleware enforcing --http-auth.
// Returns nil if --http-open is set. Returns error if --http-auth is missing.
func (b *Bgpipe) httpAuthMiddleware() (func(http.Handler) http.Handler, error) {
	if b.K.Bool("http-open") {
		return nil, nil
	}

	authStr := strings.TrimSpace(b.K.String("http-auth"))
	if authStr == "" {
		return nil, fmt.Errorf("--http requires --http-auth (or --http-open to disable auth)")
	}

	cred, err := b.readCredential(authStr)
	if err != nil {
		return nil, fmt.Errorf("--http-auth: %w", err)
	}
	expected := []byte("Basic " + base64.StdEncoding.EncodeToString(cred))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), expected) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="bgpipe"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// readCredential reads a credential from a string, $ENV variable, or /path.
func (b *Bgpipe) readCredential(v string) ([]byte, error) {
	if len(v) > 1 && v[0] == '$' {
		return []byte(os.Getenv(v[1:])), nil
	}
	if len(v) > 0 && v[0] == '/' {
		fh, err := os.Open(v)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, 128)
		n, err := fh.Read(buf)
		fh.Close()
		if err != nil {
			return nil, fmt.Errorf("file %s: %w", v, err)
		}
		cred, _, _ := bytes.Cut(buf[:n], []byte{'\n'})
		return cred, nil
	}
	return []byte(v), nil
}

func (b *Bgpipe) configurePprof(m *chi.Mux) error {
	pprofVal := strings.TrimSpace(b.K.String("pprof"))
	if pprofVal == "" {
		return nil
	}

	// separate pprof server? overwrite m with a fresh mux
	if pprofVal != "http" {
		m = chi.NewMux()
	}

	m.HandleFunc("/debug/pprof/*", pprof.Index)
	m.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	m.HandleFunc("/debug/pprof/profile", pprof.Profile)
	m.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	m.HandleFunc("/debug/pprof/trace", pprof.Trace)

	if pprofVal == "http" {
		b.httppprof = true
		return nil
	}

	// start separate pprof server (no auth)
	ln, err := net.Listen("tcp", pprofVal)
	if err != nil {
		return fmt.Errorf("could not bind --pprof %s: %w", pprofVal, err)
	}

	go func() {
		srv := &http.Server{Handler: m, ReadHeaderTimeout: 5 * time.Second}
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			b.Warn().Err(err).Msg("pprof server error")
		}
	}()

	b.Info().Msgf("pprof: http://%s/debug/pprof/", ln.Addr())
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

	b.Info().Msgf("HTTP API: http://%s/", ln.Addr())
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

// attachHTTPStages mounts per-stage HTTP routes on the shared mux.
func (b *Bgpipe) attachHTTPStages() {
	if b.httpmux == nil {
		return
	}

	m := b.httpmux
	used := make(map[string]struct{})

	for _, s := range b.Stages {
		if s == nil {
			continue
		}

		r := chi.NewRouter()
		if err := s.Stage.RouteHTTP(r); err != nil {
			s.Warn().Err(err).Msg("could not register HTTP API")
			continue
		}
		if len(r.Routes()) == 0 {
			continue
		}

		base := s.HTTPSlug()
		if _, exists := used[base]; exists {
			base = fmt.Sprintf("%s-%d", base, s.Index)
		}
		used[base] = struct{}{}

		s.HTTPPath = "/stage/" + base
		m.Mount(s.HTTPPath, r)

		s.Info().Str("http", s.HTTPPath).Msg("stage HTTP API mounted")
	}
}

func (b *Bgpipe) httpDashboard(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(b.StartTime).Truncate(time.Second)

	// collect stage info
	type stageInfo struct {
		Index    int
		Name     string
		Cmd      string
		Dir      string
		HTTPPath string
	}
	var stages []stageInfo
	for _, s := range b.Stages {
		if s == nil {
			continue
		}
		stages = append(stages, stageInfo{
			Index:    s.Index,
			Name:     s.Name,
			Cmd:      s.Cmd,
			Dir:      s.StringLR(),
			HTTPPath: s.HTTPPath,
		})
	}

	// render pipeline text (like --explain)
	var pipeR, pipeL bytes.Buffer
	b.StageDump(1, &pipeR) // DIR_R = 1
	b.StageDump(2, &pipeL) // DIR_L = 2

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>bgpipe %s</title>
<style>
  :root { --bg: #0d1117; --fg: #c9d1d9; --accent: #58a6ff; --card: #161b22; --border: #30363d; --dim: #8b949e; --green: #3fb950; }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif; background: var(--bg); color: var(--fg); min-height: 100vh; padding: 2rem; }
  .container { max-width: 900px; margin: 0 auto; }
  h1 { font-size: 1.5rem; margin-bottom: 0.25rem; }
  h1 span { color: var(--accent); }
  .subtitle { color: var(--dim); font-size: 0.875rem; margin-bottom: 1.5rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem; margin-bottom: 1.5rem; }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: 8px; padding: 1rem; }
  .card .label { color: var(--dim); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .card .value { font-size: 1.25rem; font-weight: 600; margin-top: 0.25rem; }
  .card .value.ok { color: var(--green); }
  h2 { font-size: 1rem; color: var(--dim); margin-bottom: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .pipeline { background: var(--card); border: 1px solid var(--border); border-radius: 8px; padding: 1rem; margin-bottom: 1.5rem; font-family: 'SF Mono', SFMono-Regular, Consolas, 'Liberation Mono', Menlo, monospace; font-size: 0.8125rem; white-space: pre; overflow-x: auto; color: var(--dim); line-height: 1.5; }
  table { width: 100%%; border-collapse: collapse; margin-bottom: 1.5rem; }
  th, td { text-align: left; padding: 0.5rem 0.75rem; border-bottom: 1px solid var(--border); font-size: 0.875rem; }
  th { color: var(--dim); font-weight: 500; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  a { color: var(--accent); text-decoration: none; }
  a:hover { text-decoration: underline; }
  .links { display: flex; gap: 1.5rem; flex-wrap: wrap; }
  .links a { background: var(--card); border: 1px solid var(--border); border-radius: 6px; padding: 0.5rem 1rem; font-size: 0.875rem; }
  .links a:hover { border-color: var(--accent); text-decoration: none; }
</style>
</head>
<body>
<div class="container">
  <h1><span>bgpipe</span> dashboard</h1>
  <p class="subtitle">BGP pipeline processor</p>

  <div class="grid">
    <div class="card"><div class="label">Version</div><div class="value">%s</div></div>
    <div class="card"><div class="label">Uptime</div><div class="value">%s</div></div>
    <div class="card"><div class="label">Stages</div><div class="value">%d</div></div>
    <div class="card"><div class="label">Status</div><div class="value ok">Running</div></div>
  </div>

  <h2>Pipeline</h2>
  <div class="pipeline">`, html.EscapeString(b.Version),
		html.EscapeString(b.Version),
		html.EscapeString(uptime.String()),
		b.StageCount())

	fmt.Fprintf(&buf, "--&gt; Messages flowing right --&gt;\n%s\n&lt;-- Messages flowing left &lt;--\n%s",
		html.EscapeString(pipeR.String()),
		html.EscapeString(pipeL.String()))

	fmt.Fprintf(&buf, `</div>

  <h2>Stages</h2>
  <table>
    <tr><th>#</th><th>Name</th><th>Command</th><th>Direction</th><th>HTTP</th></tr>`)

	for _, s := range stages {
		httpCol := "-"
		if s.HTTPPath != "" {
			httpCol = fmt.Sprintf(`<a href="%s/">%s/</a>`, s.HTTPPath, s.HTTPPath)
		}
		fmt.Fprintf(&buf, "\n    <tr><td>%d</td><td>%s</td><td>%s</td><td><code>%s</code></td><td>%s</td></tr>",
			s.Index,
			html.EscapeString(s.Name),
			html.EscapeString(s.Cmd),
			html.EscapeString(s.Dir),
			httpCol)
	}

	fmt.Fprintf(&buf, `
  </table>

  <h2>Links</h2>
  <div class="links">
    <a href="/metrics">Prometheus Metrics</a>
    <a href="/hc">Health Check</a>`)

	if b.httppprof {
		fmt.Fprintf(&buf, `
    <a href="/debug/pprof/">pprof</a>`)
	}

	fmt.Fprintf(&buf, `
    <a href="https://bgpipe.org">Documentation</a>
  </div>
</div>
</body>
</html>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}
