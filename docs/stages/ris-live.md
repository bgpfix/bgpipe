# ris-live

Stream BGP updates from RIPE RIS Live.

## Synopsis

```
bgpipe [...] -- ris-live [OPTIONS]
```

## Description

The **ris-live** stage connects to the
[RIPE NCC RIS Live](https://ris-live.ripe.net/) streaming service and injects
real-time BGP updates into the pipeline. RIS Live aggregates BGP data from
[RIPE RIS route collectors](https://www.ripe.net/analyse/internet-measurements/routing-information-service-ris)
worldwide, providing a global view of Internet routing without requiring
your own BGP peering sessions.

The stage connects via Server-Sent Events (SSE) to the RIS Live endpoint,
extracts raw BGP messages from the stream, and injects them into the pipeline.
Each message is tagged with peer and collector metadata:

| Tag | Description |
|-----|-------------|
| `PEER_IP` | IP address of the BGP peer |
| `PEER_AS` | AS number of the BGP peer |
| `RIS_ID` | RIS collector peer ID |
| `RIS_HOST` | RIS collector hostname (e.g., `rrc01.ris.ripe.net`) |
| `COLLECTOR` | Short collector name derived from RIS_HOST (e.g., `rrc01`) |

These tags can be used in downstream [filters](../filters.md), for example:
`tag[PEER_AS] == 13335` or `tag[RIS_HOST] ~ "rrc01"`.

Connection retries are enabled by default. The stage monitors message
freshness and treats stale messages (older than `--delay-err`) as connection
errors, triggering a reconnect.

Use `--sub` to pass a
[RIS Live subscription filter](https://ris-live.ripe.net/manual/#ris_subscribe)
to limit the data at the source. The subscription JSON must include
`"includeRaw": true` for bgpipe to process the raw BGP messages.

The global `-g` / `--guess-asn` flag is recommended when using this stage,
as different RIS peers may use 2-byte or 4-byte ASNs.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--url` | string | *(RIS Live endpoint)* | Override the streaming endpoint URL |
| `--sub` | string | | [RIS Subscribe](https://ris-live.ripe.net/manual/#ris_subscribe) JSON filter |
| `--timeout` | duration | `10s` | Connect timeout; 0 disables |
| `--read-timeout` | duration | `10s` | Max time between messages before reconnecting |
| `--retry` | bool | `true` | Retry connection on errors |
| `--retry-max` | int | `0` | Max retry attempts; 0 means unlimited |
| `--delay-err` | duration | `3m` | Treat messages older than this as errors; 0 disables |

## Examples

Stream all RIS Live updates and print to stdout:

```bash
bgpipe -go -- ris-live
```

Monitor a specific prefix in real-time:

```bash
bgpipe -go -- ris-live -- grep 'prefix ~ 1.1.1.0/24'
```

Subscribe to a specific collector:

```bash
bgpipe -go -- ris-live --sub '{"host":"rrc01","includeRaw":true}'
```

Stream with RPKI validation, show only invalid:

```bash
bgpipe -go \
    -- ris-live \
    -- rpki --invalid keep \
    -- grep 'tag[rpki/status] == INVALID'
```

Archive RIS Live to compressed files with hourly rotation:

```bash
bgpipe -g \
    -- ris-live \
    -- write --every 1h 'ris-live.$TIME.mrt.gz'
```

## See Also

[rv-live](rv-live.md),
[read](read.md),
[RIPE RIS Live](https://ris-live.ripe.net/),
[Stages overview](index.md)
