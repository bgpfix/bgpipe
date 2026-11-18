# mrai

Send prefix announcements at most once per time window (MRAI).

## Synopsis

```
bgpipe [...] -- mrai [OPTIONS]
```

## Description

The **mrai** stage holds the announcements of each peer, and sends them once
per `--window`, counted from the first one held: for each prefix, only its last
announcement. This is the BGP MRAI timer
([RFC 4271](https://datatracker.ietf.org/doc/html/rfc4271#section-9.2.1.1)):
the transient paths that a peer tries during path exploration ("path hunting",
eg. before a withdrawal) are not sent. Withdrawals are sent at once, and cancel
the held announcements of their prefixes.

The last state of every prefix is kept, but changes faster than the window are
not: use it to reduce churn (eg. for monitoring, alerting, or feeding a router),
not when you need every update (eg. for research on path exploration).

Each peer has its own clock, taken from message timestamps, so the stage works
the same on live feeds and on files. A peer is identified as in
[combine](combine.md). If a peer sends nothing for a window, its held
announcements are sent. UPDATEs are sent as received, without the prefixes
announced again later; add [combine](combine.md) to repack them.

Only IPv4 and IPv6 unicast UPDATEs that only announce, or only withdraw, are
held. All other messages (eg. KEEPALIVE, End-of-RIB, Flowspec) are passed on
right away, after the held announcements of their peer.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `-w`, `--window` | duration | `5s` | Min. time between announcements of a prefix |

## Examples

Remove most path hunting from RIPE RIS Live, then repack the UPDATEs:

```bash
bgpipe -o -- ris-live -- mrai -- combine
```

On 60 seconds of RIS Live (all collectors): 21% fewer prefix announcements, the
same last state of every prefix, and 60% fewer path hunting episodes (2+ paths,
then a withdrawal); 31% fewer UPDATEs, 36% with `combine`.

## See Also

[combine](combine.md),
[ris-live](ris-live.md),
[rv-live](rv-live.md),
[Stages overview](index.md)
