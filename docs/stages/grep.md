# grep / drop

Filter messages by matching against a BGP filter expression.

## Synopsis

```
bgpipe [...] -- grep [OPTIONS] FILTER
bgpipe [...] -- drop [OPTIONS] FILTER
```

## Description

The **grep** stage keeps messages that match *FILTER* and drops the rest.
The **drop** stage does the inverse: it drops matching messages and keeps
the rest. Both are the same stage with opposite default behavior.

The *FILTER* argument is a BGP filter expression that tests message attributes
such as prefix, AS path, communities, and tags. See the
[Message Filters](../filters.md) reference for complete syntax.

This stage supports bidirectional operation with `-LR`. Without `-LR`, it
processes only messages in the stage direction. It matches UPDATE messages by
default; non-UPDATE messages (OPEN, KEEPALIVE, NOTIFICATION) are dropped unless explicitly allowed.

With `--keep`, the stage evaluates the filter but never drops messages.
This requires at least one of `--event-match` or `--event-fail` so that
the filter evaluation has a visible effect.

As an alternative to a separate **grep** stage, most consumer stages support
the `--if` flag, which skips the stage for messages that don't match the
filter. For example, `stdout --if 'ipv6'` prints only IPv6 updates.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--keep` | bool | `false` | Evaluate the filter but never drop (requires `--event-*`) |
| `--event-match` | string | | Emit this event when a message matches |
| `--event-fail` | string | | Emit this event when a message does not match |
| `--kill-match` | bool | `false` | Kill the process on filter match |
| `--kill-fail` | bool | `false` | Kill the process on filter failure |

## Examples

Keep only IPv6 updates:

```bash
bgpipe -o -- read updates.mrt.gz -- grep 'ipv6'
```

Drop updates with AS path containing AS64512:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- drop 'as_path ~ ,64512,' \
    -- connect 10.0.0.1
```

Filter updates by origin AS and prefix:

```bash
bgpipe -o -- read updates.mrt.gz -- grep 'as_origin == 15169 && prefix ~ 8.8.0.0/16'
```

Emit an event on match without dropping:

```bash
bgpipe --events grep/hijack \
    -- connect 192.0.2.1 \
    -- grep --keep --event-match hijack 'prefix ~ 192.0.2.0/24 && as_origin != 64496' \
    -- connect 10.0.0.1
```

Kill the session if a default route is announced:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- grep --kill-match default 'prefix == 0.0.0.0/0' \
    -- connect 10.0.0.1
```

Use `--if` on a stage instead of a separate grep:

```bash
bgpipe -o -- read updates.mrt.gz -- stdout --if 'as_origin == 15169'
```

## See Also

[Message Filters](../filters.md),
[tag](tag.md),
[limit](limit.md),
[Stages overview](index.md)
