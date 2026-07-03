# HTTP API

bgpipe can expose an HTTP API alongside the pipeline: a live dashboard, Prometheus metrics,
a health check endpoint, per-stage JSON summaries, and optional Go profiling. This is a
global feature, independent of any particular stage.

## Enabling

Set the global `--http` option to a bind address:

```bash
bgpipe --http :8080 --http-open \
    -- connect 192.0.2.1 \
    -- rov \
    -- connect 10.0.0.1
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--http` | string | | Bind address for the HTTP API (e.g. `:8080`, `127.0.0.1:8080`) |
| `--http-auth` | string | | HTTP Basic Auth credentials: `user:pass`, `$ENV_VAR`, or `/path/to/file` |
| `--http-open` | bool | `false` | Disable authentication entirely (dangerous -- see [Authentication](#authentication)) |
| `--pprof` | string | | Enable Go profiling: `http` to mount under `--http`, or a separate `addr` for its own server |

`--http` only binds the listener; it does not attach a stage. Use the [metrics](stages/metrics.md)
stage if you also want labeled, filter-based Prometheus counters.

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `/` | Web dashboard: version, uptime, stage list, and links to the endpoints below |
| `/metrics` | Prometheus text format, covering all stages and the Go runtime |
| `/hc` | JSON health check (`status`, `version`, `stages`, `uptime`) -- suitable for Kubernetes liveness/readiness probes |
| `/stage/<name>/` | Per-stage JSON summary, for stages that implement one (see [Per-stage routes](#per-stage-routes)) |
| `/debug/pprof/` | Go pprof profiles, only mounted when `--pprof` is set |

## Authentication

Because `/debug/pprof/` and per-stage routes can expose internal state, `--http` refuses to
start unless you pick one of:

- `--http-auth user:pass` (or `--http-auth $ENV_VAR` / `--http-auth /path/to/file`) --
  requires HTTP Basic Auth on every request, compared in constant time.
- `--http-open` -- disables authentication. Only use this when `--http` is bound to
  `127.0.0.1` or an otherwise trusted network.

```bash
# credentials from an environment variable
export BGPIPE_HTTP_AUTH="admin:s3cret"
bgpipe --http :8080 --http-auth '$BGPIPE_HTTP_AUTH' \
    -- connect 192.0.2.1 -- rov -- connect 10.0.0.1
```

NB: when `--pprof` is given a separate address (not `http`), that dedicated pprof server
runs without authentication, since it is expected to be bound to a private interface.

## Per-stage routes

A stage can register its own routes under `/stage/<name>/`, where `<name>` is the stage's
name (or command, for unnamed stages; use `@name` on the stage to set it explicitly). Stages
that currently expose a JSON summary:

| Stage | Path | Contents |
|-------|------|----------|
| [rov](stages/rov.md) | `/stage/rov/` | Loaded VRP counts, messages/valid/invalid/not_found counters |
| [aspa](stages/aspa.md) | `/stage/aspa/` | Loaded ASPA record count, messages/valid/unknown/invalid counters |
| [metrics](stages/metrics.md) | `/stage/metrics/` | Total message count and per-rule filter counters |

If two stages resolve to the same slug, the second is suffixed with its pipeline index
(e.g. `/stage/rov-3/`).

## Profiling

`--pprof http` mounts the standard `net/http/pprof` handlers under `/debug/pprof/` on the
same `--http` listener (subject to the same auth). `--pprof :6060` instead starts a
dedicated, unauthenticated pprof server on that address -- convenient for a quick
`go tool pprof` session on a host you already trust:

```bash
bgpipe --http :8080 --http-open --pprof http \
    -- read updates.mrt.gz -- write output.json
# then: go tool pprof http://localhost:8080/debug/pprof/profile
```

## Examples

Dashboard and health check for a live proxy, bound to localhost only:

```bash
bgpipe --http 127.0.0.1:8080 --http-open \
    -- listen :179 \
    -- rov \
    -- connect 192.0.2.1
# open http://127.0.0.1:8080/ for the dashboard
# curl http://127.0.0.1:8080/hc for a health check
```

Prometheus scraping with authentication, plus labeled filter counters via `metrics`:

```bash
bgpipe --http :9090 --http-auth /etc/bgpipe/http-auth \
    -- connect 192.0.2.1 \
    -- metrics -LR -A ipv4 ipv6 'bogon: as_origin > 64512' \
    -- connect 10.0.0.1
```

## See Also

[metrics](stages/metrics.md),
[rov](stages/rov.md),
[aspa](stages/aspa.md),
[Quick Start](quickstart.md)
