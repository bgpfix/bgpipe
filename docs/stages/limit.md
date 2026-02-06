# limit

Enforce prefix length and count limits.

## Synopsis

```
bgpipe [...] -- limit [OPTIONS]
```

## Description

The **limit** stage enforces constraints on BGP UPDATE messages to prevent
prefix flooding attacks (such as the [Kirin attack](https://kirin-attack.github.io/)).
It tracks announced prefixes and drops messages that violate configured limits.

The stage supports three types of count limits:

- **Session limit** (`--session`): maximum total prefixes across the session
- **Origin limit** (`--origin`): maximum prefixes per origin AS
- **Block limit** (`--block`): maximum prefixes per IP address block

Prefix length limits (`--min-length`, `--max-length`) reject individual
prefixes that are too specific or too broad.

Use `--ipv4` and/or `--ipv6` to select which address families to process.
If neither is specified, only IPv4 is processed. Use separate **limit** stages
for different limits on IPv4 and IPv6.

The stage tracks prefix state across the session lifetime, correctly handling
both announcements and withdrawals (unless `--permanent` is set).

This stage supports bidirectional operation with `-LR`, aggregating counts
from both directions. Without `-LR`, it tracks only the stage direction.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `-4`, `--ipv4` | bool | `false` | Process IPv4 prefixes (default if neither -4 nor -6 given) |
| `-6`, `--ipv6` | bool | `false` | Process IPv6 prefixes |
| `--multicast` | bool | `false` | Include multicast address families |
| `--permanent` | bool | `false` | Ignore withdrawals (announcements are permanent) |
| `-m`, `--min-length` | int | `0` | Minimum prefix length; 0 disables |
| `-M`, `--max-length` | int | `0` | Maximum prefix length; 0 disables |
| `-s`, `--session` | int | `0` | Global session prefix limit; 0 disables |
| `-o`, `--origin` | int | `0` | Per-AS origin prefix limit; 0 disables |
| `-b`, `--block` | int | `0` | Per-IP-block prefix limit; 0 disables |
| `-B`, `--block-length` | int | `0` | IP block prefix length (max 64); 0 means /16 (IPv4) or /32 (IPv6) |

## Events

The stage emits events prefixed with `limit/` when limits are violated:

| Event | Trigger |
|-------|---------|
| `limit/short` | Prefix shorter than `--min-length` |
| `limit/long` | Prefix exceeds `--max-length` |
| `limit/session` | Session prefix count exceeds `--session` |
| `limit/origin` | Per-origin prefix count exceeds `--origin` |
| `limit/block` | Per-block prefix count exceeds `--block` |

Use `--events limit/session` on bgpipe to log these events, or
`--kill limit/session` to terminate the session when a limit is hit.

## Examples

Enforce standard prefix length limits for IPv4 and IPv6:

```bash
bgpipe --kill limit/session \
    -- connect 192.0.2.1 \
    -- limit -LR -4 --min-length 8 --max-length 24 --session 1000000 \
    -- limit -LR -6 --min-length 16 --max-length 48 --session 250000 \
    -- connect 10.0.0.1
```

Limit per-origin AS prefix count (detect prefix hijack floods):

```bash
bgpipe --events limit/origin \
    -- connect 192.0.2.1 \
    -- limit -LR --origin 5000 \
    -- connect 10.0.0.1
```

Limit per /16 block (IPv4) to detect concentrated floods:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- limit --block 10000 --block-length 16 \
    -- connect 10.0.0.1
```

## See Also

[grep](grep.md),
[rpki](rpki.md),
[Kirin Attack](https://kirin-attack.github.io/),
[Stages overview](index.md)
