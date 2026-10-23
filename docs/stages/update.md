# update

Modify UPDATE message attributes.

## Synopsis

```
bgpipe [...] -- update [OPTIONS]
```

## Description

The **update** stage modifies BGP UPDATE messages in-flight, with the route
policy actions common in production networks: rewrite next-hops, add, remove
or scrub communities, prepend or clean the AS_PATH, set MED, LOCAL_PREF and
ORIGIN, and drop unwanted attributes. Non-UPDATE messages pass through
unchanged.

This stage supports bidirectional operation with `-LR`. Without `-LR`, it
applies only to messages in the stage direction. Combine with `--if` to apply
modifications only to messages matching a [filter](../filters.md).

## Options

### Next-hop

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--nexthop4` | string | | Set next-hop for IPv4 prefixes to this address |
| `--nexthop6` | string | | Set next-hop for IPv6 prefixes to this address |
| `--nexthop-self` | bool | `false` | Set next-hop to our own IP address (when available) |

### Communities

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--add-com` | string | | Add a standard BGP community (`ASN:value`) |
| `--add-com-ext` | string | | Add an extended BGP community (JSON, see below) |
| `--add-com-large` | string | | Add a large BGP community (`G:L1:L2`) |
| `--drop-com` | bool | `false` | Remove the COMMUNITY attribute entirely |
| `--drop-com-ext` | bool | `false` | Remove the EXT_COMMUNITY attribute entirely |
| `--drop-com-large` | bool | `false` | Remove the LARGE_COMMUNITY attribute entirely |
| `--del-com` | strings | | Remove matching communities (CSV); `*` matches any value, eg. `65000:*` |
| `--del-com-large` | strings | | Remove matching large communities (CSV), eg. `65000:*:*` |

Unlike its siblings, `--add-com-ext` takes the JSON object format used in the
EXT_COMMUNITY attribute output, e.g. `'{"type": "TARGET", "value": "65000:100"}'`
(or a JSON array of such objects).

For each attribute, drop takes precedence: when both `--drop-com*` and the
matching `--add-com*` are given, the attribute is dropped and the add is
ignored. `--del-com*` runs after `--add-com*`.

### AS_PATH

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--prepend` | strings | | Prepend given ASN(s) to the AS_PATH (CSV, in order), eg. `65000,65000` |
| `--prepend-first` | int | `0` | Prepend the first (neighbor) ASN given number of times (0-32) |
| `--strip-private` | bool | `false` | Remove private ASNs ([RFC 6996](https://datatracker.ietf.org/doc/html/rfc6996)) from the AS_PATH |

`--prepend-first` is applied before `--prepend`, eg. `--prepend-first 2
--prepend 65000` turns `3549 3356` into `65000 3549 3549 3549 3356`. AS_SET
segments are left as-is. All AS_PATH actions assume 4-byte ASN sessions (the
norm): on legacy 2-byte sessions, AS4_PATH is not modified.

### Other attributes

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--set-med` | int | | Set MULTI_EXIT_DISC (MED) to given value |
| `--drop-med` | bool | `false` | Remove the MED attribute (takes precedence over `--set-med`) |
| `--set-localpref` | int | | Set LOCAL_PREF (only meaningful towards iBGP peers) |
| `--set-origin` | string | | Set ORIGIN: `igp`, `egp`, or `incomplete` |
| `--drop-attr` | strings | | Remove given attributes (CSV of names or codes), eg. `OTC,28` |

### Message size

An edit can grow an UPDATE past the maximum BGP message size: 4096 bytes, or
65535 if Extended Message ([RFC 8654](https://datatracker.ietf.org/doc/html/rfc8654))
was negotiated. Since sending it would reset the session downstream, such an
UPDATE is converted into a withdrawal of its prefixes instead (with a warning).

Messages without reachable prefixes (pure withdrawals) are passed through
unmodified: per RFC 4271 section 4.3, a withdrawal must not carry path
attributes.

## Examples

Rewrite next-hop for all updates:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- update --nexthop4 10.0.0.1 \
    -- connect 10.0.0.1
```

Add a community to tag traffic from a specific peer:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- update --add-com 65000:100 \
    -- connect 10.0.0.1
```

Strip all communities before forwarding:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- update --drop-com --drop-com-ext --drop-com-large \
    -- connect 10.0.0.1
```

Conditionally modify: add a community only to RPKI-invalid updates:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- rov --invalid keep \
    -- update --if 'tag[rov/status] == INVALID' --add-com 65000:666 \
    -- connect 10.0.0.1
```

Set next-hop to self (useful when proxying):

```bash
bgpipe \
    -- listen :179 \
    -- update --nexthop-self \
    -- connect 192.0.2.1
```

Scrub your own (eg. informational or action) communities received from
external peers, and strip private ASNs leaked by a customer:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- update --del-com '65000:*' --del-com-large '65000:*:*' --strip-private \
    -- connect 10.0.0.1
```

Traffic engineering: make routes towards a peer less attractive by prepending
our ASN twice, and announce a lower preference via MED:

```bash
bgpipe \
    -- listen --peer 10.0.0.1 :179 \
    -- update -L --prepend 65000,65000 --set-med 100 \
    -- connect 192.0.2.1
```

Graceful shutdown before maintenance ([RFC 8326](https://datatracker.ietf.org/doc/html/rfc8326)):
tag all routes with `GRACEFUL_SHUTDOWN` so that peers depreference them:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- update --add-com 65535:0 \
    -- connect 10.0.0.1
```

Drop optional attributes known to break older routers (eg. attribute 28):

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- update --drop-attr 28 \
    -- connect 10.0.0.1
```

## See Also

[grep](grep.md),
[rov](rov.md),
[Stages overview](index.md)
