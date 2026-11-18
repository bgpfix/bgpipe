# combine

Combine UPDATEs with the same attributes into one.

## Synopsis

```
bgpipe [...] -- combine [OPTIONS]
```

## Description

The **combine** stage holds UPDATEs for a short time, and merges those from the
same peer, with the same path attributes, into one UPDATE with all their
prefixes. Withdrawals from the same peer are merged too. This is what BGP
speakers normally do, and what some feeds undo: eg. [rv-live](rv-live.md)
delivers one prefix per message, while the RouteViews MRT archives hold the
original UPDATEs.

Nothing changes but how prefixes are packed into messages: for each prefix,
every announcement and withdrawal is kept, in order. A pending UPDATE is sent
after `--window`, or when adding a prefix would make it larger than
`--max-size`. If a prefix changes again before that, the pending UPDATEs of its
peer are sent first.

A peer is identified by the message direction, and the `COLLECTOR`, `PEER_IP`
and `PEER_AS` [tags](../json-format.md) (set by eg. read, bmp, rv-live and
ris-live): only UPDATEs of the same peer are combined, and the combined UPDATE
has the tags of the first one. Only IPv4 and IPv6 unicast UPDATEs that only announce,
or only withdraw, prefixes are combined. All other messages (eg. KEEPALIVE,
End-of-RIB, Flowspec) are passed on unchanged, after the pending UPDATEs of
their peer, so the order of messages is kept.

The stage passes all messages through its own input, so they continue from the
next stage. When the stages before it are done (eg. at the end of a file), it
sends all pending UPDATEs and stops.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `-w`, `--window` | duration | `1s` | Max. time to hold an UPDATE |
| `--max-size` | int | `4096` | Max. size of a combined UPDATE (bytes) |

## Examples

Reconstruct UPDATEs from the RouteViews live stream:

```bash
bgpipe -o -- rv-live -- combine
```

Merge a dump into fewer, larger UPDATEs, eg. before replaying it to a router:

```bash
bgpipe -- read updates.20240212.0745.bz2 -- combine --window 10s -- write combined.mrt
```

## See Also

[rv-live](rv-live.md),
[mrai](mrai.md),
[update](update.md),
[Stages overview](index.md)
