# bmp

Collect or export a BMP feed (BGP Monitoring Protocol, RFC 7854) over TCP.

## Synopsis

```
bgpipe [...] -- bmp [OPTIONS] ADDR
```

## Description

The **bmp** stage speaks BMP over TCP in either role:

- **station** (default): reads a BMP feed and injects the BGP UPDATEs it
  carries into the pipeline;
- **exporter** (`--write`): sends the UPDATEs seen in the pipeline as a BMP
  feed, like a monitored router would.

In both roles the stage can connect to *ADDR*, or wait for connections on it
with `--listen`. Routers usually connect to their station, so a typical
station runs `bmp --listen :11019`, and a typical exporter `bmp --write
station:11019`. With `--listen`, any number of peers can connect at the same
time; use `--peer` to limit who can.

NB: BMP has no authentication. A station injects whatever routes a connected
peer sends, and an exporter sends the pipeline's routes to anyone who connects.
Always use `--peer` with `--listen` (the stage warns without it), and TLS
(`--cert`/`--key`, `--tls`) on untrusted networks.

To read or write BMP *files*, use [read](read.md) and [write](write.md) with
`--format bmp`.

### Station

Each UPDATE is tagged with where it comes from:

| Tag | Description |
|-----|-------------|
| `PEER_IP`, `PEER_AS` | The monitored BGP peer |
| `PEER_ID` | BGP Identifier of the peer |
| `BMP_RIB` | RIB view: `in-post`, `out-pre`, `out-post`, or `loc` (not set for pre-policy Adj-RIB-In) |
| `PEER_RD` | Peer distinguisher (hex), if non-zero |
| `ROUTER` | sysName of the monitored router (its IP address if not sent) |

Use them in filters, eg. `--of 'tag[BMP_RIB] == loc'`.

The stage follows Peer Up and Peer Down notifications: ADD-PATH is taken per
peer and address family, as negotiated in its BGP session, and the AS number
size per peer. Peer Up and Peer Down also become events (see below), and
Statistics Reports become Prometheus gauges.

### Exporter

A station first gets an Initiation message, then a Peer Up ahead of the first
UPDATE of each peer, and a Termination message when the pipeline ends. The
peer is taken from the message tags listed above, which is what
[read](read.md) (MRT, BMP), [ris-live](ris-live.md), and [rv-live](rv-live.md)
set. For a live BGP session it is the neighbor that sent the UPDATE: its
address, and the AS number and BGP Identifier from its OPEN.

For a live BGP session, Peer Up carries the real OPEN messages of the session,
so the station sees its true capabilities, hold time, and ADD-PATH; otherwise
the OPENs are synthesized, announcing ADD-PATH where the exported UPDATEs
carry path IDs. Only UPDATEs are exported, and there is no initial table dump:
a station sees what flows through the pipeline after it connects. Without
`--listen`, the stage connects before the pipeline starts, so eg. a file
replay is exported from its first UPDATE. A station that reads too slowly is
disconnected (see `--buffer`), so it never stalls the pipeline. When the
pipeline stops, each station gets up to 5 seconds to receive what is queued
for it.

To write a BMP session to a file instead, use [write](write.md) with
`--format bmp`: the file can be replayed to any station later.

### Events

| Event | Arguments | Trigger |
|-------|-----------|---------|
| `bmp/peer-up` | router, peer IP, peer AS | Peer Up notification from a router |
| `bmp/peer-down` | router, peer IP, peer AS | Peer Down notification from a router |

Use eg. `--events bmp/peer-down` on bgpipe to log them, or `exec` a script on
them. After a Peer Down, all routes of that peer are implicitly withdrawn;
the stage does not keep a RIB, so it does not send the withdrawals.

### Prometheus Metrics

Each Statistics Report counter or gauge is exported as:

```
bgpipe_bmp_stats{router="192.0.2.1",peer="198.51.100.7",type="7"} 912345
```

`router` is the IP address of the router connection, `peer` the monitored
peer, and `type` the BMP Statistics Type (RFC 7854 section 4.8, eg. 7 for
routes in Adj-RIB-In). Per address family gauges (eg. type 9) also get an
`af` label, eg. `af="IPV6/UNICAST"`. See [HTTP API](../http-api.md).

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--listen` | bool | `false` | Listen on *ADDR* instead of connecting to it |
| `--write` | bool | `false` | Export: send UPDATEs as a BMP feed instead of reading one |
| `--peer` | strings | | With `--listen`: accept only these IPs/prefixes |
| `--cert` | string | | With `--listen`: TLS certificate path (enables TLS) |
| `--key` | string | | With `--listen`: TLS private key path |
| `--tls` | bool | `false` | Connect over TLS |
| `--insecure` | bool | `false` | Do not validate TLS certificates |
| `--timeout` | duration | `15s` | TCP connect timeout (0 means off) |
| `--keepalive` | duration | `15s` | TCP keepalive period (-1 means off) |
| `--retry` | bool | `false` | Retry on connection errors (also refused connections), and reconnect when the session ends |
| `--retry-max` | int | `0` | Maximum number of connection retries (0 means unlimited) |
| `--sys-name` | string | `bgpipe` | sysName to send in export mode |
| `--buffer` | int | `100000` | Export: max. queued messages per client before it is dropped |

## Examples

BMP station for two routers, printing JSON:

```bash
bgpipe --stdout -- bmp --listen --peer 192.0.2.1,192.0.2.2 :11019
```

Post-policy routes only, archived as MRT:

```bash
bgpipe \
    -- bmp --listen --peer 192.0.2.0/24 :11019 \
    -- grep 'tag[BMP_RIB] == in-post' \
    -- write --every 15m 'bmp.$TIME.mrt.gz'
```

Read a BMP restream (eg. Netom `bmp-tcp-out`) over TLS, reconnect when needed:

```bash
bgpipe --stdout -- bmp --tls --retry collector.example.net:11020
```

Replay an MRT file into a BMP station:

```bash
bgpipe \
    -- read updates.20250601.0000.bz2 \
    -- bmp --write station.example.net:11019
```

Monitor a live BGP session: proxy it, and export what both neighbors send
(each becomes a BMP peer):

```bash
bgpipe \
    -- listen :179 \
    -- bmp --write -LR station.example.net:11019 \
    -- connect --wait listen 192.0.2.1
```

Fan out RIPE RIS Live to any BMP station that connects:

```bash
bgpipe -- ris-live -- bmp --write --listen --peer 10.0.0.0/8 :11019
```
