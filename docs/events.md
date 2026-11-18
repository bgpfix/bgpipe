# Events

Events report what happens in a pipeline: the BGP session comes up, a stage
connects, a prefix breaks a limit. They are not messages: they never flow
through the pipeline. You use them on the command line to log what happened,
to start or stop stages at the right moment, or to kill the session.

| Option | Where | Effect |
|--------|-------|--------|
| `-e`, `--events` *events* | global | Log the events (default: `PARSE,NOTIFY,ESTABLISHED,EOR`; `all` for all) |
| `-k`, `--kill` *events* | global | Kill the session on any of the events (exit code `3`) |
| `-W`, `--wait` *events* | any stage | Start the stage only after one of the events |
| `-S`, `--stop` *events* | any stage | Stop the stage after one of the events |

Separate multiple events with commas, eg. `--events ESTABLISHED,limit/session`.

## Event Names

| You write | Means | Example |
|-----------|-------|---------|
| `NAME` (any capital letter) | the BGP session event `NAME` | `EOR` = `bgpfix/pipe.EOR` |
| `stage` (all lowercase) | the lifecycle events of a stage, see below | `--wait listen` = `listen/START` |
| `stage/name` | the event `name` of a stage | `limit/session`, `listen/READY` |
| `all` or `*` | all events | `--events all` |

A stage is named after its command (eg. [listen](stages/listen.md),
[limit](stages/limit.md)). Mind the case: a
lowercase name with no slash is a stage, so `--events established` waits for a
stage called `established`. Write the BGP session events in capitals.

## BGP Session Events

The pipeline emits these as messages reach its ends, in a given direction.

| Event | When |
|-------|------|
| `START` | The pipeline has started |
| `STOP` | The pipeline is about to stop |
| `OPEN` | An OPEN reached the end of the pipeline |
| `ESTABLISHED` | The session is established: OPEN and KEEPALIVE made it both ways |
| `UPDATE` | An UPDATE reached the end of the pipeline (at most once per second) |
| `ALIVE` | A KEEPALIVE reached the end of the pipeline (at most once per second) |
| `EOR_AF` | End-of-RIB for one address family |
| `EOR` | End-of-RIB for all negotiated address families |
| `NOTIFY` | A NOTIFICATION reached the end of the pipeline |
| `PARSE` | A message could not be parsed |

## Stage Lifecycle Events

Every stage emits these, eg. `connect/READY`.

| Event | When |
|-------|------|
| `PREPARE` | The stage starts preparing (eg. [connect](stages/connect.md) dials its peer, [listen](stages/listen.md) waits for one) |
| `READY` | The stage is prepared (eg. the TCP connection is up) |
| `START` | The stage starts its work, once all stages started with it are ready |
| `STOP` | The stage has stopped |

A lowercase stage name stands for `START` in `--wait`, for `STOP` in `--stop`
and `--kill`, and for all four in `--events`.

## Stage Events

| Event | Arguments | When | Stage |
|-------|-----------|------|-------|
| `limit/long` | prefix, origin AS | Prefix longer than `--max-length` | [limit](stages/limit.md#events) |
| `limit/short` | prefix, origin AS | Prefix shorter than `--min-length` | [limit](stages/limit.md#events) |
| `limit/session` | prefix, origin AS, count | Too many prefixes in the session (`--session`) | [limit](stages/limit.md#events) |
| `limit/origin` | prefix, origin AS, count | Too many prefixes from one origin AS (`--origin`) | [limit](stages/limit.md#events) |
| `limit/block` | prefix, origin AS, count | Too many prefixes in one IP block (`--block`) | [limit](stages/limit.md#events) |
| `bmp/peer-up` | router, peer IP, peer AS | A monitored peer is up | [bmp](stages/bmp.md#events) |
| `bmp/peer-down` | router, peer IP, peer AS | A monitored peer is down | [bmp](stages/bmp.md#events) |
| `grep/`*name* | message | A message matches (`--event-match name`) or fails (`--event-fail name`) | [grep](stages/grep.md) |
| `rov/`*name* | message | RPKI INVALID prefixes (`--event name`) | [rov](stages/rov.md) |
| `aspa/`*name* | message | ASPA INVALID path (`--event name`) | [aspa](stages/aspa.md) |

[grep](stages/grep.md), [rov](stages/rov.md) and [aspa](stages/aspa.md) emit events only if you name
them, eg. `rov --event dropped` emits `rov/dropped`. Run `bgpipe STAGE -h` to see the
events of a stage.

## Examples

Log the session coming up, and every prefix limit violation:

```bash
bgpipe --events ESTABLISHED,limit/session \
  -- connect 192.0.2.1 \
  -- limit --session 100000 \
  -- connect 198.51.100.1
```

Kill the session when a peer announces too many prefixes:

```bash
bgpipe --kill limit/session \
  -- listen :179 \
  -- limit -LR --session 1000000 \
  -- connect 198.51.100.1
```

Connect to the upstream router only after the downstream one connects:

```bash
bgpipe -- listen :179 -- connect --wait listen 198.51.100.1
```

Replay a file to a router after the session is up, then stop:

```bash
bgpipe \
  -- speaker --active --asn 65000 --stop read/STOP \
  -- read --wait ESTABLISHED updates.mrt \
  -- connect 192.0.2.1
```

Logged events look like this:

```
INF event bgpfix/pipe.ESTABLISHED evseq=12 vals=[1700000000]
INF event limit/long evseq=90 stage="[2] limit" vals=["138.186.235.5/32",53062]
```

## See Also

[Stages](stages/index.md),
[Message Tags](tags.md),
[Examples](examples.md)
