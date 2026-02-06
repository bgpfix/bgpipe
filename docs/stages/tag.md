# tag

Add or remove message tags.

## Synopsis

```
bgpipe [...] -- tag [OPTIONS]
```

## Description

The **tag** stage manipulates key-value pairs (tags) attached to BGP messages
as they flow through the pipeline. Tags are metadata that travel with messages
but are not part of the BGP wire format. They appear in the `meta` field of
the [JSON representation](../json-format.md).

Tags are useful for:

- Annotating messages with pipeline context (e.g., source collector, timestamp)
- Passing information between stages (e.g., from [rpki](rpki.md) to [grep](grep.md))
- Filtering based on custom criteria using `tag[key]` in [filter expressions](../filters.md)

This stage supports bidirectional operation with `-LR`. Without `-LR`, it
applies only to messages in the stage direction.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--add` | strings | | Add tags in `key=value` format |
| `--drop` | strings | | Drop tags by key; use `*` to drop all tags |
| `--src` | bool | `false` | Add a `SRC` tag with the source stage name |

## Examples

Add a tag to all messages:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- tag --add source=upstream1 \
    -- write -LR session.json \
    -- connect 10.0.0.1
```

Strip all tags before writing:

```bash
bgpipe -- read tagged-data.json -- tag --drop '*' -- write clean.json
```

Tag and filter: add environment tag, then filter on it downstream:

```bash
bgpipe -o \
    -- read updates.mrt.gz \
    -- tag --add env=prod \
    -- grep 'tag[env] == prod'
```

## See Also

[grep](grep.md),
[Message Filters](../filters.md),
[JSON Format](../json-format.md),
[Stages overview](index.md)
