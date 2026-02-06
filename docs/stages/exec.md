# exec

Pipe messages through an external process.

## Synopsis

```
bgpipe [...] -- exec [OPTIONS] COMMAND
bgpipe [...] -- exec -A COMMAND [ARGS...] --
```

## Description

The **exec** stage runs an external command and exchanges BGP messages with it
over stdin/stdout. Pipeline messages are serialized (JSON by default) and sent
to the command's stdin; the command's stdout is parsed back into messages and
injected into the pipeline.

This makes it straightforward to process BGP data with any language - Python,
Perl, shell scripts, or compiled programs. The external process receives one
message per line and can:

- **Filter**: print only messages it wants to keep
- **Modify**: alter message contents and print the result
- **Generate**: produce new messages on stdout
- **Drop**: simply not print a message to discard it

The command's stderr is forwarded to the bgpipe log.

The stage is both a producer and a consumer, and supports bidirectional
operation with `-LR`.

Use the `-A` / `--args` flag to pass arguments to the command. Without `-A`,
only the first argument after `exec` is treated as the command path. With `-A`,
all arguments up to the next `--` are passed to the command.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--keep-stdin` | bool | `false` | Keep running if the command's stdin is closed |
| `--keep-stdout` | bool | `false` | Keep running if the command's stdout is closed |
| `--format` | string | `json` | Data format: `json`, `raw`, `mrt`, `exa`, `bmp`, or `obmp` |
| `--type` | strings | | Process only messages of given type(s) |
| `--skip` | strings | | Skip messages of given type(s) |
| `--read` | bool | `false` | Read-only mode (don't send pipeline output to command) |
| `--write` | bool | `false` | Write-only mode (don't read command output) |
| `--copy` | bool | `false` | Mirror messages instead of consuming them |
| `--pardon` | bool | `false` | Ignore input parsing errors |
| `--no-seq` | bool | `false` | Overwrite input sequence numbers |
| `--no-time` | bool | `false` | Overwrite input timestamps |
| `--no-tags` | bool | `false` | Drop input message tags |

## Examples

Filter updates with a Python script:

```bash
bgpipe -o -- read updates.mrt.gz -- exec -A python3 filter.py --
```

Use sed to rewrite ASNs in OPEN messages:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- exec -LR -A sed -ure '/"OPEN"/{ s/65055/65001/g }' -- \
    -- connect 10.0.0.1
```

Process with ExaBGP-compatible scripts:

```bash
bgpipe \
    -- listen :179 \
    -- exec --format exa -LR -A /path/to/script.py -- \
    -- connect 192.0.2.1
```

Write-only: send messages to a monitoring script without reading back:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- exec --write -LR -A /usr/local/bin/bgp-logger -- \
    -- connect 10.0.0.1
```

### Python Script Example

A Python script that filters IPv4 prefixes shorter than /16:

```python
#!/usr/bin/env python3
import sys, json

for line in sys.stdin:
    msg = json.loads(line)
    if msg[3] == "UPDATE":
        if "reach" in msg[4]:
            msg[4]["reach"] = [
                p for p in msg[4]["reach"]
                if int(p.split("/")[1]) < 16
            ]
            if not msg[4]["reach"]:
                continue
    print(json.dumps(msg), flush=True)
```

## See Also

[pipe](pipe.md),
[websocket](websocket.md),
[JSON Format](../json-format.md),
[Stages overview](index.md)
