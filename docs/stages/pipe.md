# pipe

Exchange messages through a named pipe (FIFO).

## Synopsis

```
bgpipe [...] -- pipe [OPTIONS] PATH
```

## Description

The **pipe** stage reads and writes BGP messages through a named pipe (FIFO).
It is both a producer and a consumer, and supports bidirectional operation
with `-LR`.

The *PATH* argument specifies the path to a named pipe created with
`mkfifo(1)`. The stage opens the pipe for both reading and writing
simultaneously.

Messages are exchanged in JSON format by default (one per line).

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--format` | string | `json` | Data format: `json`, `raw`, `mrt`, `exa`, `bmp`, or `obmp` |
| `--type` | strings | | Process only messages of given type(s) |
| `--skip` | strings | | Skip messages of given type(s) |
| `--read` | bool | `false` | Read-only mode |
| `--write` | bool | `false` | Write-only mode |
| `--copy` | bool | `false` | Mirror messages instead of consuming them |
| `--pardon` | bool | `false` | Ignore input parsing errors |
| `--no-seq` | bool | `false` | Overwrite input sequence numbers |
| `--no-time` | bool | `false` | Overwrite input timestamps |
| `--no-tags` | bool | `false` | Drop input message tags |

## Examples

Process messages through a named pipe:

```bash
mkfifo /tmp/bgp-pipe
bgpipe \
    -- connect 192.0.2.1 \
    -- pipe -LR /tmp/bgp-pipe \
    -- connect 10.0.0.1
```

Write-only: send messages to a pipe for external monitoring:

```bash
mkfifo /tmp/bgp-monitor
bgpipe \
    -- connect 192.0.2.1 \
    -- pipe --write -LR /tmp/bgp-monitor \
    -- connect 10.0.0.1
```

## See Also

[exec](exec.md),
[websocket](websocket.md),
[Stages overview](index.md)
