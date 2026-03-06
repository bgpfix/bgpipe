# metrics

Count messages matching filter expressions and expose Prometheus metrics.

## Synopsis

```
bgpipe --http :8080 [...] -- metrics [OPTIONS] [-A] [LABEL: FILTER] ...
```

## Description

The **metrics** stage counts BGP messages flowing through the pipeline
and exposes them as Prometheus counters. It always tracks generic counters
(total messages, by direction, by message type), and optionally evaluates
user-defined filter expressions with labeled counters.

Requires `--http` to expose metrics via HTTP. The global `/metrics` endpoint
serves all Prometheus metrics in text format. The stage also mounts a JSON
summary at `/stage/metrics/`.

### Filter arguments

Each positional argument defines a filter rule in the format `LABEL: FILTER`.
If no `: ` (colon-space) separator is present, the argument text is used as
both label and filter expression. Labels are sanitized to `[a-z0-9_]` for
Prometheus compatibility.

```bash
bgpipe --http :8080 \
  -- connect 192.0.2.1 \
  -- metrics -LR -A \
      ipv4 \
      'v6: ipv6' \
      'google: as_origin == 15169' \
      'long_path: aspath_len > 5' \
  -- connect 10.0.0.1
```

This creates counters like:

```
bgpipe_metrics_match{filter="ipv4"} 42
bgpipe_metrics_match{filter="v6"} 18
bgpipe_metrics_match{filter="google"} 5
bgpipe_metrics_match{filter="long_path"} 3
```

### Batch mode

For offline analysis (e.g., measuring an MRT file), use `--output` to write
the final counter values to a file when the pipeline exits. This captures
metrics that would otherwise be lost because the pipeline finishes before
a scraper can read `/metrics`.

Place the `metrics` stage **after** the source stage so it sees the messages:

```bash
bgpipe \
  -- read updates.mrt \
  -- metrics --output results.prom -A ipv4 ipv6
grep bgpipe results.prom
```

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--output` | string | | Write final metrics to file on exit (Prometheus text format) |

## Generic counters

These are always created (prefix derived from stage name):

| Counter | Description |
|---------|-------------|
| `bgpipe_metrics_messages_total` | Total messages seen (all directions/types) |
| `bgpipe_metrics_messages_total{dir="left",type="update"}` | UPDATE messages flowing left |
| `bgpipe_metrics_messages_total{dir="right",type="update"}` | UPDATE messages flowing right |
| `bgpipe_metrics_messages_total{dir="left",type="open"}` | OPEN messages flowing left |
| `bgpipe_metrics_messages_total{dir="right",type="keepalive"}` | KEEPALIVE messages flowing right |
| *(all dir × type combinations)* | One series per direction+type pair |

Each `(dir, type)` combination is a separate Prometheus series, enabling PromQL
aggregations like `sum by (dir)`, `sum by (type)`, or filtering on
`{dir="right",type="update"}`.

## HTTP endpoints

When `--http` is set:

| Endpoint | Description |
|----------|-------------|
| `/` | Web dashboard with pipeline info, stages, uptime, and links |
| `/metrics` | Global Prometheus metrics (all stages, all Go runtime) |
| `/hc` | Health check (JSON, k8s-compatible) |
| `/stage/metrics/` | JSON summary of this stage's counters |
| `/debug/pprof/` | Go pprof (when `--pprof` is enabled) |

## Examples

Monitor a live BGP session with Prometheus scraping:

```bash
bgpipe --http :9090 \
    -- connect 192.0.2.1 \
    -- metrics -LR -A ipv4 ipv6 'bogon: as_origin > 64512' \
    -- connect 10.0.0.1
```

Measure an MRT dump offline and save results:

```bash
bgpipe \
    -- read updates.20230301.0000.mrt \
    -- metrics --output stats.prom -A \
        ipv4 ipv6 'google: as_origin == 15169' \
    -- write updates.20230301.json
```

Named stage with custom metric prefix:

```bash
bgpipe --http :8080 \
    -- connect 192.0.2.1 \
    -- @my_counters metrics -LR -A ipv4 ipv6 \
    -- connect 10.0.0.1
# produces: bgpipe_my_counters_messages_total, bgpipe_my_counters_match{filter="ipv4"}, etc.
```

## See Also

[Message Filters](../filters.md),
[grep](grep.md),
[rpki](rpki.md),
[Stages overview](index.md)
