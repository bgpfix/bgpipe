# rv-live

Stream BGP updates from RouteViews via Kafka.

## Synopsis

```
bgpipe [...] -- rv-live [OPTIONS]
```

## Description

The **rv-live** stage consumes real-time BGP updates from the
[University of Oregon RouteViews](https://www.routeviews.org/) project via
its public Kafka stream. RouteViews collects BGP data from routers at
major Internet exchange points and transit providers worldwide.

The stream carries BGP messages in OpenBMP raw format. The stage parses
BMP messages, extracts the inner BGP messages, and injects them into the
pipeline with peer metadata as tags:

| Tag | Description |
|-----|-------------|
| `PEER_IP` | IP address of the BGP peer |
| `PEER_AS` | AS number of the BGP peer |
| `COLLECTOR` | RouteViews collector name (e.g., `linx`) |
| `ROUTER` | Router IP address |

These tags can be used in downstream [filters](../filters.md), for example:
`tag[COLLECTOR] ~ "linx"` or `tag[PEER_AS] == 13335`.

By default, the stage subscribes to all topics matching the pattern
`^routeviews\..+\.bmp_raw$`, which covers all RouteViews collectors.
Use `--collector` to select specific collectors by name prefix, or
`--topics` to supply a custom topic regex.

RouteViews injects the collector's own AS into the AS_PATH of each
message. By default, this stage strips that first hop. Use `--keep-aspath`
to preserve the original AS_PATH as received from the collector.

The `--state` option enables offset persistence: the stage saves its Kafka
consumer position to a file and resumes from the same point on restart.
This prevents re-processing messages after a pipeline restart.

Connection retries are enabled by default. The stage monitors data
freshness and reconnects when no data arrives for `--stale` duration.

The global `-g` / `--guess-asn` flag is recommended when using this stage,
as different RouteViews peers may use 2-byte or 4-byte ASNs.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--broker` | string | `stream.routeviews.org:9092` | Kafka broker address |
| `--topics` | string | `^routeviews\..+\.bmp_raw$` | Topic regex pattern |
| `--collector` | strings | | Only subscribe to collectors whose name starts with this prefix |
| `--collector-not` | strings | | Exclude collectors whose name starts with this prefix |
| `--group` | string | *(auto-generated)* | Kafka consumer group ID |
| `--state` | string | | State file for offset persistence |
| `--refresh` | duration | `5m` | Topic list refresh interval |
| `--timeout` | duration | `30s` | Connection timeout |
| `--stale` | duration | `3m` | Reconnect if no data for this long; 0 disables |
| `--retry` | bool | `true` | Retry connection on errors |
| `--retry-max` | int | `0` | Max retry attempts; 0 means unlimited |
| `--keep-aspath` | bool | `false` | Keep the collector AS in AS_PATH |

## Examples

Stream all RouteViews updates to stdout:

```bash
bgpipe -go -- rv-live
```

Stream from a specific collector:

```bash
bgpipe -go -- rv-live --collector linx
```

Exclude specific collectors:

```bash
bgpipe -go -- rv-live --collector-not amsix --collector-not decix
```

Persist consumer state for resumable processing:

```bash
bgpipe -go -- rv-live --state /var/lib/bgpipe/rv-live.state
```

Archive RouteViews data with RPKI validation:

```bash
bgpipe -g \
    -- rv-live \
    -- rpki --invalid keep \
    -- write --every 1h 'rv-updates.$TIME.json.gz'
```

## See Also

[ris-live](ris-live.md),
[read](read.md),
[RouteViews](https://www.routeviews.org/),
[Stages overview](index.md)
