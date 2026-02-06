# listen

Accept an incoming BGP connection over TCP.

## Synopsis

```
bgpipe [...] -- listen [OPTIONS] [ADDR]
```

## Description

The **listen** stage binds to a local TCP address and waits for a single
incoming BGP connection. It is both a producer and a consumer: it reads BGP
messages from the connected client and writes pipeline messages back to it.

The optional *ADDR* argument specifies the listen address as `host:port`,
`:port`, or `host`. Defaults to `:179` (all interfaces, standard BGP port).

Once a client connects, the listener closes and no further connections are
accepted.

By default, **listen** messages flow left-to-right (`-R` direction), but if it is the last stage in the pipeline, it defaults to the left (`-L`) direction, so that incoming messages flow right-to-left.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--timeout` | duration | `0` | Accept timeout; 0 means wait indefinitely |
| `--closed-timeout` | duration | `1s` | TCP half-closed timeout; 0 disables |
| `--keepalive` | duration | `15s` | TCP keepalive period; -1 disables |
| `--md5` | string | | TCP MD5 password (Linux only) |

## Examples

Listen on the default BGP port and proxy to an upstream router:

```bash
bgpipe -- connect --wait listen 192.0.2.1 -- listen :179
```

Listen on a non-standard port for local BIRD integration:

```bash
bgpipe -- connect --wait listen --md5 "s3cret" 192.0.2.1 -- listen localhost:1790
```

Add TCP-MD5 to a session (listen without, connect with):

```bash
bgpipe -o -- listen :179 -- connect --md5 "s3cret" 10.0.0.1
```

## See Also

[connect](connect.md),
[speaker](speaker.md),
[Stages overview](index.md)
