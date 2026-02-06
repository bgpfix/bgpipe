# websocket

Exchange messages over WebSocket.

## Synopsis

```
bgpipe [...] -- websocket [OPTIONS] URL
```

## Description

The **websocket** stage sends and receives BGP messages over a WebSocket
connection. It supports both client mode (connecting to a remote server)
and server mode (`--listen`), with optional TLS encryption and HTTP basic
authentication.

The *URL* argument specifies the WebSocket endpoint. Schemes `ws://` and
`wss://` are used directly; `http://` and `https://` are automatically
converted to `ws://` and `wss://` respectively.

In **server mode** (`--listen`), the stage accepts multiple concurrent
WebSocket clients and broadcasts messages to all connected clients.
If a non-critical client disconnects, the stage continues operating.
In server mode with `wss://`, both `--cert` and `--key` are required.

In **client mode** (default), the stage connects to a single remote server.
Use `--retry` to automatically reconnect on connection failures.

Incoming messages from WebSocket peers are tagged with `websocket/remote`
containing the remote address, which can be used in downstream
[filters](../filters.md) (e.g., `tag[websocket/remote] ~ "10.0."`).

This stage is both a producer and a consumer, and supports bidirectional
operation with `-LR`.

## Options

### Connection

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--listen` | bool | `false` | Run as WebSocket server instead of client |
| `--timeout` | duration | `10s` | Connect/handshake timeout; 0 disables |
| `--retry` | bool | `false` | Retry client connection on errors |
| `--retry-max` | int | `0` | Max retry attempts; 0 means unlimited |

### Authentication and TLS

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--auth` | string | | HTTP basic auth; `$ENV_VAR` or file path containing `user:pass` |
| `--cert` | string | | TLS certificate file path (required for `wss://` server) |
| `--key` | string | | TLS private key file path (required for `wss://` server) |
| `--insecure` | bool | `false` | Skip TLS certificate validation |
| `--header` | strings | | Additional HTTP headers (`Header:Value` format) |

### Data Format

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

Stream a BGP session to a remote archiver (write-only):

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- websocket --write -LR wss://monitor.example.com/bgp \
    -- connect 10.0.0.1
```

Connect with authentication from an environment variable:

```bash
export BGP_AUTH="user:s3cret"
bgpipe \
    -- connect 192.0.2.1 \
    -- websocket --write -LR --auth '$BGP_AUTH' wss://monitor.example.com/bgp \
    -- connect 10.0.0.1
```

Run a WebSocket server for remote clients:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- websocket -LR --listen --cert server.crt --key server.key wss://0.0.0.0:8443/bgp \
    -- connect 10.0.0.1
```

Client with retry (reconnects on disconnection):

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- websocket -LR --retry wss://monitor.example.com/bgp \
    -- connect 10.0.0.1
```

## See Also

[exec](exec.md),
[pipe](pipe.md),
[Stages overview](index.md)
