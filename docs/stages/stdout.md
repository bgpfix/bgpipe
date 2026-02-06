# stdout

Write messages to standard output.

## Synopsis

```
bgpipe [...] -- stdout [OPTIONS]
```

## Description

The **stdout** stage writes BGP messages to standard output. It is a consumer
stage that supports bidirectional operation with `-LR`; without `-LR`, it
prints only messages in the stage direction.

The stage always mirrors messages - it never consumes them from the pipeline,
so downstream stages still see every message that stdout prints.

The default output format is JSON (one message per line). Use `--format` to
select MRT, raw, ExaBGP line, BMP, or OpenBMP format instead.

As a shorthand, the global `-o` / `--stdout` flag adds an implicit **stdout**
stage at the end of the pipeline. The `-O` / `--stdout-wait` variant waits
for `EVENT_EOR` (End of RIB) before printing.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--format` | string | `json` | Data format: `json`, `raw`, `mrt`, `exa`, `bmp`, or `obmp` |
| `--type` | strings | | Print only messages of given type(s) |
| `--skip` | strings | | Skip messages of given type(s) |

## Examples

Print all messages as JSON:

```bash
bgpipe -- read updates.mrt.gz -- stdout
```

Print only after the BGP session is established:

```bash
bgpipe -O -- speaker --active --asn 65001 -- connect 192.0.2.1
```

Print in ExaBGP line format:

```bash
bgpipe -- read updates.mrt.gz -- stdout --format exa
```

Print only UPDATE messages:

```bash
bgpipe -- read updates.mrt.gz -- stdout --type UPDATE
```

## See Also

[stdin](stdin.md),
[write](write.md),
[JSON Format](../json-format.md),
[Stages overview](index.md)
