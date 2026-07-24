# update

Modify UPDATE message attributes.

## Synopsis

```
bgpipe [...] -- update [OPTIONS]
```

## Description

The **update** stage modifies BGP UPDATE messages in-flight. It can rewrite
next-hop addresses and manipulate community attributes. Non-UPDATE messages
pass through unchanged.

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

Unlike its siblings, `--add-com-ext` takes the JSON object format used in the
EXT_COMMUNITY attribute output, e.g. `'{"type": "TARGET", "value": "65000:100"}'`
(or a JSON array of such objects).

For each attribute, drop takes precedence: when both `--drop-com*` and the
matching `--add-com*` are given, the attribute is dropped and the add is
ignored.

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

## See Also

[grep](grep.md),
[rov](rov.md),
[Stages overview](index.md)
