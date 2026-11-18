# smooth

Hold announcements, send only the last one for each prefix.

## Synopsis

```
bgpipe [...] -- smooth [OPTIONS]
```

## Description

The **smooth** stage holds each announced prefix for `--window`, counted from
its first announcement, and then sends its last announcement only. This is
similar to the BGP MRAI timer ([RFC 4271](https://datatracker.ietf.org/doc/html/rfc4271#section-9.2.1.1)):
a prefix is announced at most once per window, so the transient paths a peer
tries during path exploration ("path hunting", eg. before a withdrawal) are
not sent. Withdrawals are sent at once, and cancel the pending announcements of
their prefixes.

The last state of every prefix is kept, but changes faster than the window are
not: use it to reduce churn (eg. for monitoring, alerting, or feeding a router),
not when you need every update (eg. for research on path exploration).

Each peer is handled on its own, with its own clock taken from message
timestamps, so the stage works the same on live feeds and on files. A peer is
identified as in [combine](combine.md). If a peer sends nothing for a window,
its pending announcements are sent. Announcements are sent packed as in their
last UPDATEs; add [combine](combine.md) to repack them.

Only IPv4 and IPv6 unicast UPDATEs that only announce, or only withdraw, are
smoothed. All other messages (eg. KEEPALIVE, End-of-RIB, Flowspec) are passed
on unchanged, after the pending announcements of their peer.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `-w`, `--window` | duration | `5s` | Time to hold an announcement |

## Examples

Remove most path hunting from RIPE RIS Live, then repack the UPDATEs:

```bash
bgpipe -o -- ris-live -- smooth -- combine
```

On 60 seconds of RIS Live (all collectors), `smooth` sent 21% fewer prefix
announcements with the same last state of every prefix, and 86% fewer path
hunting episodes (2+ paths, then a withdrawal); with `combine`, 39% fewer UPDATEs.

## See Also

[combine](combine.md),
[ris-live](ris-live.md),
[rv-live](rv-live.md),
[Stages overview](index.md)
