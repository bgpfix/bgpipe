# rpki

Validate UPDATE messages using RPKI.

## Synopsis

```
bgpipe [...] -- rpki [OPTIONS]
```

## Description

The **rpki** stage validates BGP UPDATE messages against RPKI (Resource Public
Key Infrastructure) data. It checks whether the origin AS is authorized to
announce each prefix, based on ROA (Route Origin Authorization) records.

Each prefix in an UPDATE is assigned one of three RPKI validation states:

- **VALID** - a ROA exists and matches the origin AS and prefix length
- **INVALID** - a ROA exists but the origin AS or prefix length does not match
- **NOT_FOUND** - no ROA covers this prefix

The stage obtains ROA data either from an RTR (RPKI-to-Router) server or
from a local ROA file. By default, it connects to Cloudflare's public RTR
server at `rtr.rpki.cloudflare.com:8282`.

The `--invalid` option controls how INVALID prefixes are handled:

| Action | Behavior |
|--------|----------|
| `withdraw` | Move invalid prefixes to the withdrawn list ([RFC 7606](https://datatracker.ietf.org/doc/html/rfc7606)) |
| `filter` | Remove invalid prefixes from the reachable list |
| `drop` | Drop the entire UPDATE if any prefix is invalid |
| `split` | Split invalid prefixes into a separate UPDATE that withdraws them |
| `keep` | Keep invalid prefixes unchanged (tag only) |

When `--tag` is enabled (the default), the stage adds `rpki/status` to
message tags, which can be used in downstream [filters](../filters.md)
(e.g., `tag[rpki/status] == INVALID`).

With `--strict`, prefixes with NOT_FOUND status are treated the same as
INVALID. This is an aggressive policy that only allows prefixes with
explicit RPKI authorization.

The stage waits for the ROA cache to be populated before processing messages
(unless `--asap` is set), ensuring no messages are validated against an
incomplete cache.

## Options

### RTR Connection

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--rtr` | string | `rtr.rpki.cloudflare.com:8282` | RTR server address (`host:port`) |
| `--rtr-refresh` | duration | `1h` | RTR cache refresh interval |
| `--rtr-retry` | duration | `10m` | RTR retry interval on errors |
| `--timeout` | duration | `15s` | Connect timeout; 0 disables |
| `--retry` | bool | `true` | Retry connection on errors |
| `--retry-max` | int | `0` | Max retry attempts; 0 means unlimited |
| `--tls` | bool | `false` | Connect to RTR server over TLS |
| `--insecure` | bool | `false` | Skip TLS certificate validation |
| `--no-ipv6` | bool | `false` | Avoid IPv6 when connecting to RTR server |

### ROA File

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--file` | string | | Use a local ROA file instead of RTR (JSON or CSV, auto-reloaded) |

### Validation Policy

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--invalid` | string | `withdraw` | Action for INVALID prefixes: `withdraw`, `filter`, `drop`, `split`, `keep` |
| `--strict` | bool | `false` | Treat NOT_FOUND same as INVALID |
| `--tag` | bool | `true` | Add `rpki/status` to message tags |
| `--event` | string | | Emit this event on RPKI INVALID messages |
| `--asap` | bool | `false` | Start validating before ROA cache is ready |

## Examples

Basic RPKI filtering between two routers (default: withdraw invalid):

```bash
bgpipe \
    -- listen :179 \
    -- rpki \
    -- connect 192.0.2.1
```

Keep invalid prefixes but tag them for downstream processing:

```bash
bgpipe -o \
    -- ris-live \
    -- rpki --invalid keep \
    -- grep 'tag[rpki/status] == INVALID'
```

Strict mode: only allow RPKI-VALID prefixes:

```bash
bgpipe --events rpki/dropped \
    -- listen :179 \
    -- rpki --strict --invalid drop --event dropped \
    -- connect 192.0.2.1
```

Use a local ROA file instead of RTR:

```bash
bgpipe \
    -- listen :179 \
    -- rpki --file /var/lib/rpki/roas.json --invalid filter \
    -- connect 192.0.2.1
```

Tag with RPKI status and add a community to invalid routes:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- rpki --invalid keep \
    -- update --if 'tag[rpki/status] == INVALID' --add-com 65000:666 \
    -- connect 10.0.0.1
```

Connect to an RTR server over TLS:

```bash
bgpipe \
    -- listen :179 \
    -- rpki --rtr rpki.example.com:8323 --tls \
    -- connect 192.0.2.1
```

## See Also

[limit](limit.md),
[grep](grep.md),
[update](update.md),
[RFC 6811 - RPKI-Based Origin Validation](https://datatracker.ietf.org/doc/html/rfc6811),
[Stages overview](index.md)
