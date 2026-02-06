# speaker

Run a simple BGP speaker.

## Synopsis

```
bgpipe [...] -- speaker [OPTIONS]
```

## Description

The **speaker** stage implements BGP session negotiation. It handles OPEN
message exchange, KEEPALIVE generation, and hold timer management. Use it
when bgpipe needs to participate as a BGP peer rather than passively proxying
an existing session.

In **passive** mode (default), the speaker waits for the remote side to send
an OPEN message first, then responds with its own OPEN. In **active** mode
(`--active`), it sends its OPEN immediately.

The speaker automatically negotiates BGP capabilities (MP-BGP, 4-byte ASN,
Route Refresh, Extended Messages) with the remote peer. When `--asn` is set
to -1, the speaker mirrors the remote peer's ASN. When `--id` is empty, it
derives a router ID from the remote peer's ID.

A **speaker** stage is not needed when bgpipe operates as a transparent proxy
between two BGP speakers that negotiate with each other directly.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--active` | bool | `false` | Send the OPEN message first |
| `--asn` | int | `-1` | Local ASN; -1 means mirror the remote ASN |
| `--id` | string | | Router ID; empty means derive from remote |
| `--hold` | int | `90` | Hold time in seconds |

## Examples

Connect to a BGP speaker in active mode:

```bash
bgpipe -o -- speaker --active --asn 65001 -- connect 192.0.2.1
```

Passive speaker that mirrors the remote ASN (useful for testing):

```bash
bgpipe -o -- speaker -- listen :179
```

Speaker with explicit identity:

```bash
bgpipe -o -- speaker --active --asn 64512 --id 10.0.0.1 -- connect 192.0.2.1
```

Replay an MRT file into a live BGP session:

```bash
bgpipe \
    -- speaker --active --asn 65001 \
    -- read --wait ESTABLISHED updates.mrt.gz \
    -- listen :179
```

## See Also

[connect](connect.md),
[listen](listen.md),
[Stages overview](index.md)
