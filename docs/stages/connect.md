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

With `--ttl`, the outgoing TTL / hop limit is set explicitly. Use `--ttl 255`
to satisfy a peer enforcing GTSM ([RFC 5082](https://datatracker.ietf.org/doc/html/rfc5082)),
or a higher value for multihop eBGP. Note that `--ttl` and `--md5` require
Linux or OpenBSD; on other platforms these options fail at socket setup.

### Transparent mode

With `--transparent` (Linux only), **connect** enables `IP_TRANSPARENT`,
which lets it bind to (spoof) a non-local source address. Paired with a
transparent [listen](listen.md) stage, this builds a fully transparent
man-in-the-middle proxy: neither BGP speaker sees the bgpipe host at the IP
layer. The router only needs to redirect TCP/179 to the bgpipe host (TPROXY,
PBR, or an inline bridge) - no BGP reconfiguration.

In transparent mode the endpoints default to the captured TCP tuple published
by the listen side (`L_LOCAL` as the target, `L_REMOTE` as the spoofed source).
Pass `0.0.0.0` as *ADDR* to ask for the captured target, and leave `--bind`
unset to spoof the captured source. Use `-W`/`--wait` so **connect** dials
only after the listen side has accepted and published the tuple; otherwise an
explicit *ADDR* and `--bind` (which you know in advance as the administrator)
are used as-is. The MD5 password is the same on both legs - the key the two
routers already share for the pair.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--timeout` | duration | `15s` | TCP connect timeout; 0 disables |
| `--closed-timeout` | duration | `1s` | TCP half-closed timeout; 0 disables |
| `--keepalive` | duration | `15s` | TCP keepalive period; -1 disables |
| `--md5` | string | | TCP MD5 password ([RFC 2385](https://datatracker.ietf.org/doc/html/rfc2385)) |
| `--bind` | string | | Local address to bind to (`IP` or `IP:port`) |
| `--transparent` | bool | `false` | Transparent proxy mode (Linux TPROXY); see below |
| `--ttl` | int | `0` | Outgoing IP TTL / hop limit; 0 leaves the kernel default |
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

Transparent man-in-the-middle proxy (router redirects TCP/179 to this host):

```bash
bgpipe \
  -- listen  --transparent --md5 "s3cret" :179 \
  -- connect --transparent --md5 "s3cret" --ttl 255 -W listen 0.0.0.0
```

## See Also

[listen](listen.md),
[speaker](speaker.md),
[Stages overview](index.md)
