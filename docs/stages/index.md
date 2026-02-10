# Stages

A **stage** is a processing step in a bgpipe pipeline. Each stage performs a specific
task: connecting to a BGP speaker, filtering messages, writing to a file, and so on.
Stages are composed left-to-right on the command line, separated by `--`.

```
bgpipe [OPTIONS] [--] STAGE1 [OPTIONS] [ARGS] [--] STAGE2 [OPTIONS] [ARGS] ...
```

## Available Stages

### Connection

| Stage | Description |
|-------|-------------|
| [connect](connect.md) | Connect to a BGP endpoint over TCP |
| [listen](listen.md) | Accept an incoming BGP connection over TCP |
| [speaker](speaker.md) | Run a simple BGP speaker |

### Input / Output

| Stage | Description |
|-------|-------------|
| [stdin](stdin.md) | Read messages from standard input |
| [stdout](stdout.md) | Write messages to standard output |
| [read](read.md) | Read messages from a file or URL |
| [write](write.md) | Write messages to a file |

### Filtering

| Stage | Description |
|-------|-------------|
| [grep](grep.md) | Keep messages matching a filter; drop the rest |
| [drop](grep.md) | Drop messages matching a filter; keep the rest |
| [tag](tag.md) | Add or remove message tags |
| [limit](limit.md) | Enforce prefix length and count limits |
| [head](head.md) | Stop the pipeline after N messages |

### Modification

| Stage | Description |
|-------|-------------|
| [update](update.md) | Modify UPDATE message attributes |

### External Processing

| Stage | Description |
|-------|-------------|
| [exec](exec.md) | Pipe messages through an external process |
| [pipe](pipe.md) | Exchange messages through a named pipe (FIFO) |
| [websocket](websocket.md) | Exchange messages over WebSocket |

### Live Streaming

| Stage | Description |
|-------|-------------|
| [ris-live](ris-live.md) | Stream BGP updates from RIPE RIS Live |
| [rv-live](rv-live.md) | Stream BGP updates from RouteViews via Kafka |

### Security

| Stage | Description |
|-------|-------------|
| [rpki](rpki.md) | Validate UPDATE messages using RPKI |

## Common Options

Every stage accepts the following options:

| Option | Description |
|--------|-------------|
| `-L`, `--left` | Operate in the L (left) direction |
| `-R`, `--right` | Operate in the R (right) direction |
| `-A`, `--args` | Consume all remaining CLI arguments until `--` |
| `-W`, `--wait` *events* | Wait for given event(s) before starting |
| `-S`, `--stop` *events* | Stop the stage after given event(s) |
| `--rate-limit` *N* | Delay messages to stay under *N* messages/sec |
| `--rate-sample` *N* | Randomly sample messages when over *N* messages/sec |

Stages that produce messages also accept:

| Option | Description |
|--------|-------------|
| `-N`, `--new` *target* | Which stage to send new messages to (default `next`) |

In `--wait` and `--stop`, multiple events can be comma-separated. If you refer a stage name (e.g., `listen`), the event is expanded by appending `/READY` (e.g., `listen/READY`), which is emitted when the stage is ready to process messages (e.g., after accepting a new connection). You can also refer to custom events emitted by stages (e.g., `grep/match`).

Stages that support input or output filtering:

| Option | Description |
|--------|-------------|
| `-I`, `--if` *filters* | Input filter: skip capturing messages that don't match all the [filters](../filters.md) |
| `-O`, `--of` *filters* | Output filter: drop produced messages that don't match all the [filters](../filters.md) |

### Direction

By default, stages operate in the **right** (`-R`) direction, processing messages
flowing left-to-right. The last stage that connects to a BGP endpoint defaults
to the **left** (`-L`) direction instead. Use `-LR` for bidirectional processing.

## See Also

[Quick Start](../quickstart.md),
[Examples](../examples.md),
[Message Filters](../filters.md),
[JSON Format](../json-format.md)
