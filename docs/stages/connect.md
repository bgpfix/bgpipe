# connect

Connect to a BGP endpoint over TCP.

## Synopsis

```
bgpipe [...] -- connect [OPTIONS] ADDR
```

## Description

The **connect** stage establishes a TCP connection to a remote BGP speaker
at *ADDR*. It is both a producer and a consumer: it reads BGP messages from
the wire and writes pipeline messages to the remote peer.

The *ADDR* argument specifies the target as `host`, `host:port`, or `[host]:port`.
If no port is given, the default BGP port 179 is used.

By default, **connect** messages flow left-to-right (`-R` direction), but if it is the last stage in the pipeline, it defaults to the left (`-L`) direction, so that incoming messages from the remote peer flow right-to-left through the pipeline.

As a shorthand, a bare IP address can be used as a stage name instead of
writing `connect` explicitly:

```
bgpipe -o speaker -- 1.2.3.4
# equivalent to:
bgpipe -o speaker -- connect 1.2.3.4
```

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--timeout` | duration | `15s` | TCP connect timeout; 0 disables |
| `--closed-timeout` | duration | `1s` | TCP half-closed timeout; 0 disables |
| `--keepalive` | duration | `15s` | TCP keepalive period; -1 disables |
| `--md5` | string | | TCP MD5 password ([RFC 2385](https://datatracker.ietf.org/doc/html/rfc2385)) |
| `--bind` | string | | Local address to bind to (`IP` or `IP:port`) |
| `--tls` | bool | `false` | Connect over TLS |
| `--insecure` | bool | `false` | Skip TLS certificate validation |
| `--no-ipv6` | bool | `false` | Avoid IPv6 when resolving *ADDR* |
| `--retry` | bool | `false` | Retry on temporary connection errors |
| `--retry-max` | int | `0` | Max retry attempts; 0 means unlimited |

## Examples

Connect to a BGP speaker and dump the session as JSON:

```bash
bgpipe -o -- speaker --active --asn 65001 -- connect 192.0.2.1
```

Connect with TCP-MD5 authentication:

```bash
bgpipe -o -- speaker -- connect --md5 "s3cret" 192.0.2.1
```

Connect over TLS with retry:

```bash
bgpipe -o -- speaker -- connect --tls --retry 192.0.2.1:1179
```

Bind to a specific local address (multi-homed host):

```bash
bgpipe -- connect --bind 10.0.0.1 192.0.2.1 -- listen :179
```

## See Also

[listen](listen.md),
[speaker](speaker.md),
[Stages overview](index.md)
