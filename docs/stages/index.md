# Stages

A **stage** is a processing step in a bgpipe pipeline. Each stage performs a specific
task: connecting to a BGP speaker, filtering messages, writing to a file, and so on.
Stages are composed left-to-right on the command line, separated by `--`.

```
bgpipe [OPTIONS] [--] STAGE1 [OPTIONS] [ARGS] [--] STAGE2 [OPTIONS] [ARGS] ...
```

## Available Stages

| Stage | Description |
|-------|-------------|
| [aspa](aspa.md) | Validate AS paths using RPKI ASPA |
| [bmp](bmp.md) | Collect or export a BMP feed over TCP |
| [combine](combine.md) | Combine UPDATEs with the same attributes into one |
| [connect](connect.md) | Connect to a BGP endpoint over TCP |
| [drop](grep.md) | Drop messages matching a filter; keep the rest |
| [exec](exec.md) | Pipe messages through an external process |
| [grep](grep.md) | Keep messages matching a filter; drop the rest |
| [head](head.md) | Stop the pipeline after N messages |
| [limit](limit.md) | Enforce prefix length and count limits |
| [listen](listen.md) | Accept an incoming BGP connection over TCP |
| [metrics](metrics.md) | Count messages matching filters (Prometheus metrics) |
| [pipe](pipe.md) | Exchange messages through a named pipe (FIFO) |
| [read](read.md) | Read messages from a file or URL |
| [ris-live](ris-live.md) | Stream BGP updates from RIPE RIS Live |
| [rov](rov.md) | Validate route origins using RPKI ROV |
| [rv-live](rv-live.md) | Stream BGP updates from RouteViews via Kafka |
| [mrai](mrai.md) | Hold announcements, send only the last one for each prefix |
| [speaker](speaker.md) | Run a simple BGP speaker |
| [stdin](stdin.md) | Read messages from standard input |
| [stdout](stdout.md) | Write messages to standard output |
| [tag](tag.md) | Add or remove message tags |
| [update](update.md) | Modify UPDATE message attributes |
| [websocket](websocket.md) | Exchange messages over WebSocket |
| [write](write.md) | Write messages to a file |

## Common Options

Every stage accepts the following options:

| Option | Description |
|--------|-------------|
| `-L`, `--left` | Process messages flowing left (see [Direction](#direction)) |
| `-R`, `--right` | Process messages flowing right (see [Direction](#direction)) |
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

Messages flow through the pipeline in two directions: **right**, from the first
stage towards the last, and **left**, the other way. The first stage is the
left end of the pipeline, and the last stage is the right end: eg. a BGP peer,
a file, or a live stream. Two rules decide what each stage does:

1. **What it processes.** By default, a stage processes the messages flowing
   right. The first stage is the exception: nothing flows right into it, so it
   gets the messages flowing left, ie. whatever reaches the left end. Use `-R`
   or `-L` to choose the messages flowing right or left, or `-LR` for both.
2. **Where it sends new messages.** The first and the last stage send their
   messages into the pipeline, away from their end: the first to the right, the
   last to the left. Any other stage sends in the direction it processes. With
   `-LR`, messages keep their own direction (eg. the `dir` field of
   [JSON](../json-format.md)), and stages that hold messages to send them later
   (eg. [combine](combine.md), [mrai](mrai.md)) never change their direction.

For example:

| Pipeline | Stage | Processes | Sends |
|----------|-------|-----------|-------|
| `read in.mrt -- grep ... -- write out.json` | read | - | right |
| | grep | flowing right | - |
| | write | flowing right | - |
| `connect 192.0.2.1 -- update ... -- connect 198.51.100.1` | first connect | flowing left, to its peer | right: from its peer |
| | update | flowing right only (use `-LR` for both) | - |
| | last connect | flowing right, to its peer | left: from its peer |
| `read in.mrt -- exec ./filter.py` | exec | flowing right | left: the script output |
| `ris-live -- combine` | combine | flowing right | right |

With `-o` / `--stdout`, bgpipe prints the messages that reach either end of the
pipeline. Use `-n` / `--explain` to see what each stage processes and sends,
without running the pipeline.

## See Also

[Quick Start](../quickstart.md),
[Examples](../examples.md),
[Message Filters](../filters.md),
[JSON Format](../json-format.md)
