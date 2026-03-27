# rpki

Validate UPDATE messages using RPKI (ROV + ASPA).

## Synopsis

```
bgpipe [...] -- rpki [OPTIONS]
```

## Description

The **rpki** stage validates BGP UPDATE messages against RPKI (Resource Public
Key Infrastructure) data. It performs two independent checks:

**ROV (Route Origin Validation)** checks whether the origin AS is authorized
to announce each prefix, based on ROA (Route Origin Authorization) records.
Each prefix is assigned one of three states:

- **VALID** - a ROA exists and matches the origin AS and prefix length
- **INVALID** - a ROA exists but the origin AS or prefix length does not match
- **NOT_FOUND** - no ROA covers this prefix

**ASPA (Autonomous System Provider Authorization)** detects route leaks by
verifying that the AS_PATH is valley-free, using ASPA records that attest
provider/customer relationships. Each path is assigned one of three states:

- **VALID** - the path is valley-free with full cryptographic attestation
- **UNKNOWN** - the path is consistent with valley-free routing but some ASes in
  the path lack ASPA records (insufficient attestation). UNKNOWN is treated the
  same as VALID — it means "can't prove a leak" not "proven legitimate"
- **INVALID** - the path provably violates valley-free routing (route leak detected)

ASPA requires RTR v2 or a JSON file with ASPA records, and requires knowledge of
the peer's BGP role (auto-detected via [RFC 9234](https://datatracker.ietf.org/doc/html/rfc9234)
BGP Role capability, or set explicitly with `--role`).

If `--role auto` (the default) and the peer does not send the BGP Role capability
in their OPEN message, ASPA validation is silently skipped for the entire session.
Use `--role` to force ASPA validation when the peer lacks this capability.

The stage obtains ROA and ASPA data either from an RTR server (supporting
RTR v0/v1/v2 with automatic version negotiation) or from a local file.
By default, it connects to Cloudflare's public RTR v2 server at
`rtr.rpki.cloudflare.com:8282`.

The `--invalid` and `--aspa-invalid` options control how INVALID prefixes/paths
are handled:

| Action | ROV behavior | ASPA behavior |
|--------|-------------|---------------|
| `withdraw` | Move invalid prefixes to withdrawn ([RFC 7606](https://datatracker.ietf.org/doc/html/rfc7606)) | Same |
| `filter` | Remove invalid prefixes (no withdrawal) | Same as withdraw (move to withdrawn) |
| `drop` | Drop the entire UPDATE | Same |
| `split` | Split invalid prefixes into a separate withdrawing UPDATE | Same |
| `keep` | Keep invalid prefixes unchanged (tag only) | Same |

Note: for ASPA, `filter` and `withdraw` are equivalent — all reachable prefixes in the
UPDATE are moved to withdrawn, since the entire path is suspect, not individual prefixes.

When `--tag` is enabled (the default), the stage adds `rpki/status` to
message tags. When `--aspa-tag` is enabled, it adds `aspa/status`. These
can be used in downstream [filters](../filters.md)
(e.g., `tag[rpki/status] == INVALID`).

With `--strict`, prefixes with NOT_FOUND status are treated the same as
INVALID. This is an aggressive policy that only allows prefixes with
explicit RPKI authorization.

The stage waits for the ROA cache to be populated before processing messages
(unless `--asap` is set), ensuring no messages are validated against an
incomplete cache.

## Options

### RTR Connection

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--rtr` | string | `rtr.rpki.cloudflare.com:8282` | RTR server address (`host:port`) |
| `--rtr-refresh` | duration | `1h` | RTR cache refresh interval |
| `--rtr-retry` | duration | `10m` | RTR retry interval on errors |
| `--timeout` | duration | `15s` | Connect timeout; 0 disables |
| `--retry` | bool | `true` | Retry connection on errors |
| `--retry-max` | int | `0` | Max retry attempts; 0 means unlimited |
| `--tls` | bool | `false` | Connect to RTR server over TLS |
| `--insecure` | bool | `false` | Skip TLS certificate validation |
| `--no-ipv6` | bool | `false` | Avoid IPv6 when connecting to RTR server |

### ROA/ASPA File

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--file` | string | | Use a local file instead of RTR (JSON or CSV, auto-reloaded) |

The JSON format supports both ROA and ASPA records (Routinator-compatible):

```json
{
  "roas": [{"prefix": "192.0.2.0/24", "maxLength": 24, "asn": "AS65001"}],
  "aspas": [{"customer_asid": 65001, "provider_asids": [65002, 65003]}]
}
```

### ROV Validation Policy

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--invalid` | string | `withdraw` | Action for ROV INVALID: `withdraw`, `filter`, `drop`, `split`, `keep` |
| `--strict` | bool | `false` | Treat NOT_FOUND same as INVALID |
| `--tag` | bool | `true` | Add `rpki/status` to message tags |
| `--event` | string | | Emit this event on ROV INVALID messages |
| `--asap` | bool | `false` | Start validating before ROA cache is ready |

### ASPA Validation Policy

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--aspa-invalid` | string | `keep` | Action for ASPA INVALID paths: `withdraw`, `filter`, `drop`, `split`, `keep` |
| `--aspa-tag` | bool | `true` | Add `aspa/status` to message tags |
| `--aspa-event` | string | | Emit this event on ASPA INVALID messages |
| `--role` | string | `auto` | Peer BGP role for ASPA: `auto`, `provider`, `customer`, `peer`, `rs`, `rs-client` |

The `--role` flag specifies the peer's BGP role (from the peer's perspective per RFC 9234).
In `auto` mode, the role is detected from the peer's BGP Role capability in the OPEN message.
If the peer does not send a BGP Role capability, ASPA validation is silently skipped.
Set `--role` explicitly to force ASPA validation regardless of peer capabilities.

## Examples

Basic ROV + ASPA filtering (ASPA auto-detects peer role; ROV withdraws invalid):

```bash
bgpipe \
    -- listen :179 \
    -- rpki \
    -- connect 192.0.2.1
```

Keep invalid prefixes but tag them for downstream processing:

```bash
bgpipe -o \
    -- ris-live \
    -- rpki --invalid keep \
    -- grep 'tag[rpki/status] == INVALID'
```

Strict mode: only allow RPKI-VALID prefixes:

```bash
bgpipe --events rpki/dropped \
    -- listen :179 \
    -- rpki --strict --invalid drop --event dropped \
    -- connect 192.0.2.1
```

Use a local ROA file instead of RTR:

```bash
bgpipe \
    -- listen :179 \
    -- rpki --file /var/lib/rpki/roas.json --invalid filter \
    -- connect 192.0.2.1
```

Tag with RPKI status and add a community to invalid routes:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- rpki --invalid keep \
    -- update --if 'tag[rpki/status] == INVALID' --add-com 65000:666 \
    -- connect 10.0.0.1
```

Connect to an RTR server over TLS:

```bash
bgpipe \
    -- listen :179 \
    -- rpki --rtr rpki.example.com:8323 --tls \
    -- connect 192.0.2.1
```

ASPA with explicit peer role (no BGP Role capability needed):

```bash
bgpipe \
    -- listen :179 \
    -- rpki --role customer --aspa-invalid withdraw \
    -- connect 192.0.2.1
```

Tag both ROV and ASPA status without enforcing any action:

```bash
bgpipe -o \
    -- ris-live \
    -- rpki --invalid keep --aspa-invalid keep \
    -- grep 'tag[aspa/status] == INVALID'
```

## See Also

[limit](limit.md),
[grep](grep.md),
[update](update.md),
[RFC 6811 - RPKI-Based Origin Validation](https://datatracker.ietf.org/doc/html/rfc6811),
[RFC 9234 - BGP Role Capability](https://datatracker.ietf.org/doc/html/rfc9234),
[Stages overview](index.md)
