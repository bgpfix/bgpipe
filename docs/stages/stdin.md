# stdin

Read messages from standard input.

## Synopsis

```
bgpipe [...] -- stdin [OPTIONS]
```

## Description

The **stdin** stage reads BGP messages from standard input and injects them
into the pipeline. It is a producer stage that supports bidirectional
operation with `-LR`: in that case, the direction of each message is taken from
the input when available (for JSON, the `dir` field). Otherwise, messages are
injected in the stage direction.

The default input format is JSON (one message per line). Supported formats
also include MRT (BGP4MP), raw BGP wire format, ExaBGP line format,
BMP, and OpenBMP - select with `--format`.

As a shorthand, the global `-i` / `--stdin` flag adds an implicit **stdin**
stage at the beginning of the pipeline. The `-I` / `--stdin-wait` variant
waits for `EVENT_ESTABLISHED` before reading.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--format` | string | `json` | Data format: `json`, `raw`, `mrt`, `exa`, `bmp`, or `obmp` |
| `--type` | strings | | Process only messages of given type(s) |
| `--skip` | strings | | Skip messages of given type(s) |
| `--pardon` | bool | `false` | Ignore input parsing errors |
| `--no-seq` | bool | `false` | Overwrite input sequence numbers |
| `--no-time` | bool | `false` | Overwrite input timestamps |
| `--no-tags` | bool | `false` | Drop input message tags |

## Examples

Pipe JSON messages into a BGP session:

```bash
cat messages.json | bgpipe -i -- speaker --active --asn 65001 -- connect 192.0.2.1
```

Read MRT from stdin explicitly:

```bash
cat updates.mrt | bgpipe -- stdin --format mrt -- stdout
```

## See Also

[stdout](stdout.md),
[read](read.md),
[Stages overview](index.md)
