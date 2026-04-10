# rpki

Validate UPDATE messages using RPKI (ROV + ASPA).

## Synopsis

```
bgpipe [...] -- rpki [OPTIONS]
```

## Description

The **rpki** stage validates BGP UPDATE messages against RPKI (Resource Public
Key Infrastructure) data. It performs ROV by default, and optionally ASPA when
`--aspa` is set.

**ROV (Route Origin Validation,
[RFC 6811](https://datatracker.ietf.org/doc/html/rfc6811))** checks whether the
origin AS is authorized to announce each prefix, using VRPs (Validated ROA
Payloads) received from an RPKI cache server or loaded from a file. Each prefix
is assigned one of three states:

- **VALID** -- a VRP covers this prefix with a matching origin AS and maxLength
- **INVALID** -- a VRP exists for the prefix but the origin AS or length does not match
- **NOT_FOUND** -- no VRP covers this prefix

ROV in this stage assumes the origin ASN comes from the AS_PATH, so it is aimed
at eBGP edges and route-server feeds. Empty AS_PATH updates, common on iBGP or
locally originated routes, are out of scope here.

**ASPA (Autonomous System Provider Authorization,
[draft-ietf-sidrops-aspa-verification](https://datatracker.ietf.org/doc/draft-ietf-sidrops-aspa-verification/))** detects route leaks by verifying that the AS_PATH is valley-free,
using ASPA records that attest provider-customer relationships between ASes.
Each path is assigned one of three states:

- **VALID** -- the path is valley-free with full cryptographic attestation
- **UNKNOWN** -- the path appears valley-free but some ASes lack ASPA records
  (insufficient attestation, not evidence of a leak)
- **INVALID** -- the path provably violates valley-free routing (route leak)

ASPA validation is **disabled by default** and requires `--aspa` to enable. It
also requires:

1. ASPA records from an RTR v2 server
   ([draft-ietf-sidrops-8210bis](https://datatracker.ietf.org/doc/draft-ietf-sidrops-8210bis/))
   or a JSON file with ASPA data
2. Knowledge of the peer's BGP role, either auto-detected via the
   [RFC 9234](https://datatracker.ietf.org/doc/html/rfc9234) BGP Role
   capability, or set explicitly with `--aspa-role`

If `--aspa-role auto` (the default) and the peer does not send the BGP Role
capability in their OPEN message, ASPA validation is skipped for the session
and a warning is logged. Set `--aspa-role` to force ASPA validation when the
peer lacks this capability.

ASPA also verifies that the first AS in the path matches the neighbor's ASN
(per draft-ietf-sidrops-aspa-verification, Section 5). This check is skipped
for Route Server peers (`--aspa-role rs`), as RSes do not prepend their ASN
([RFC 7947](https://datatracker.ietf.org/doc/html/rfc7947)).

The stage obtains VRP and ASPA data either from an RTR server (supporting
RTR v0/v1/v2 with automatic version negotiation and fallback) or from a local
file. By default, it connects to Cloudflare's public RTR server at
`rtr.rpki.cloudflare.com:8282`.

### Actions for INVALID routes

The `--invalid` (ROV) and `--aspa-invalid` (ASPA) options control handling:

**ROV** operates per-prefix and supports all five actions:

| Action | Behavior |
|--------|----------|
| `withdraw` | Move invalid prefixes to withdrawn |
| `filter` | Remove invalid prefixes silently |
| `drop` | Drop the entire UPDATE message |
| `split` | Split invalid prefixes to a separate UPDATE with withdrawals |
| `keep` | Keep unchanged (tag only) |

**ASPA** validates the entire AS_PATH (one per UPDATE), so per-prefix actions
(`filter`, `split`) do not apply. Supported actions:

| Action | Behavior |
|--------|----------|
| `withdraw` | Move all reachable prefixes to withdrawn |
| `drop` | Drop the entire UPDATE message |
| `keep` | Keep unchanged (tag only) |

### Tags

When `--tag` is enabled (default), each prefix gets a per-prefix tag
(`rpki/<prefix>`) and the message gets an overall `rpki/status` tag.
When `--aspa-tag` is enabled (default) and ASPA is active (`--aspa`), the
message gets `aspa/status`.
These can be used in downstream [filters](../filters.md).

With `--strict`, NOT_FOUND prefixes are treated the same as INVALID.

## Options

### RTR Connection

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--rtr` | string | `rtr.rpki.cloudflare.com:8282` | RTR cache server address (`host:port`) |
| `--rtr-refresh` | duration | `1h` | Periodic Serial Query interval |
| `--rtr-retry` | duration | `10m` | Reconnection delay after failure |
| `--timeout` | duration | `15s` | TCP connect timeout (0 to disable) |
| `--retry` | bool | `true` | Retry on connection failure |
| `--retry-max` | int | `0` | Max retries (0 = unlimited) |
| `--tls` | bool | `false` | Connect over TLS |
| `--insecure` | bool | `false` | Skip TLS certificate validation |
| `--no-ipv6` | bool | `false` | Avoid IPv6 for RTR server connection |

### Local File

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--file` | string | | Local RPKI data file, auto-reloaded every 10s (note: JSON supports VRPs and ASPAs; CSV supports VRPs only) |

JSON format (Routinator-compatible: VRPs and optional ASPAs):

```json
{
  "roas": [
    {"prefix": "192.0.2.0/24", "maxLength": 24, "asn": "AS65001"},
    {"prefix": "2001:db8::/32", "maxLength": 48, "asn": 65002}
  ],
  "aspas": [
    {"customer_asid": 65001, "provider_asids": [65002, 65003]}
  ]
}
```

CSV format (VRPs only; one entry per line, `#` comments, optional header):

```csv
prefix,maxLength,asn
192.0.2.0/24,24,AS65001
2001:db8::/32,48,65002
```

### ROV Policy

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--invalid` | string | `withdraw` | Action for ROV INVALID: `withdraw`, `filter`, `drop`, `split`, `keep` |
| `--strict` | bool | `false` | Treat NOT_FOUND as INVALID |
| `--tag` | bool | `true` | Add `rpki/status` and `rpki/<prefix>` tags |
| `--event` | string | | Emit named event on ROV INVALID |
| `--no-wait` | bool | `false` | Start before VRP/ASPA cache is ready |

### ASPA Policy

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--aspa` | bool | `false` | Enable ASPA path validation |
| `--aspa-invalid` | string | `withdraw` | Action for ASPA INVALID: `withdraw`, `drop`, `keep` |
| `--aspa-tag` | bool | `true` | Add `aspa/status` tag |
| `--aspa-event` | string | | Emit named event on ASPA INVALID |
| `--aspa-role` | string | `auto` | Peer's BGP role: `auto`, `provider`, `customer`, `peer`, `rs`, `rs-client` |

The `--aspa-role` flag specifies the peer's BGP role (from the peer's
perspective, per [RFC 9234](https://datatracker.ietf.org/doc/html/rfc9234)):

| `--aspa-role` value | Meaning | ASPA direction |
|---------------|---------|----------------|
| `provider` | Peer is our provider | Downstream (route came from above) |
| `rs` | Peer is a Route Server | Upstream (first-hop ASN check is skipped) |
| `customer` | Peer is our customer | Upstream (route came from below) |
| `rs-client` | Peer is an RS client | Upstream |
| `peer` | Peer is a lateral peer | Upstream |
| `auto` | Auto-detect from BGP Role capability | Depends on detected role |

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `bgpipe_rpki_messages_total` | counter | UPDATE messages processed |
| `bgpipe_rpki_rov_valid_total` | counter | Prefixes with ROV state VALID |
| `bgpipe_rpki_rov_invalid_total` | counter | Prefixes with ROV state INVALID |
| `bgpipe_rpki_rov_not_found_total` | counter | Prefixes with ROV state NOT_FOUND |
| `bgpipe_rpki_vrps_ipv4` | gauge | IPv4 VRPs loaded |
| `bgpipe_rpki_vrps_ipv6` | gauge | IPv6 VRPs loaded |

When `--aspa` is enabled, the following are also registered:

| Metric | Type | Description |
|--------|------|-------------|
| `bgpipe_rpki_aspa_valid_total` | counter | Paths with ASPA state VALID |
| `bgpipe_rpki_aspa_unknown_total` | counter | Paths with ASPA state UNKNOWN |
| `bgpipe_rpki_aspa_invalid_total` | counter | Paths with ASPA state INVALID |
| `bgpipe_rpki_aspa_entries` | gauge | ASPA records loaded |

## Examples

Basic ROV (default: withdraw INVALID prefixes):

```bash
bgpipe \
    -- listen :179 \
    -- rpki \
    -- connect 192.0.2.1
```

ROV + ASPA with explicit peer role:

```bash
bgpipe \
    -- listen :179 \
    -- rpki --aspa --aspa-role customer \
    -- connect 192.0.2.1
```

Tag only, no enforcement -- useful for monitoring:

```bash
bgpipe -o \
    -- ris-live \
    -- rpki --invalid keep \
    -- grep 'tag[rpki/status] == INVALID'
```

Strict ROV: drop any prefix without a valid VRP:

```bash
bgpipe --events rpki/dropped \
    -- listen :179 \
    -- rpki --strict --invalid drop --event dropped \
    -- connect 192.0.2.1
```

Use a local JSON VRP/ASPA file, or a CSV VRP file, instead of RTR:

```bash
bgpipe \
    -- listen :179 \
    -- rpki --file /var/lib/rpki/export.json --invalid filter \
    -- connect 192.0.2.1
```

ROV + ASPA monitoring (tag everything, enforce nothing):

```bash
bgpipe -o \
    -- ris-live \
    -- rpki --invalid keep --aspa --aspa-invalid keep \
    -- grep 'tag[aspa/status] == INVALID'
```

Add a community to ROV-invalid routes instead of dropping:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- rpki --invalid keep \
    -- update --if 'tag[rpki/status] == INVALID' --add-com 65000:666 \
    -- connect 10.0.0.1
```

Connect to a private RTR cache over TLS:

```bash
bgpipe \
    -- listen :179 \
    -- rpki --rtr rpki.example.com:8323 --tls \
    -- connect 192.0.2.1
```

## See Also

[limit](limit.md),
[grep](grep.md),
[update](update.md),
[Stages overview](index.md)

### References

- [RFC 6811](https://datatracker.ietf.org/doc/html/rfc6811) -- RPKI-Based Route Origin Validation
- [RFC 8210](https://datatracker.ietf.org/doc/html/rfc8210) -- RPKI-to-Router Protocol (RTR v1)
- [RFC 9234](https://datatracker.ietf.org/doc/html/rfc9234) -- Route Leak Prevention and Detection Using Roles in UPDATE and OPEN Messages
- [draft-ietf-sidrops-aspa-verification](https://datatracker.ietf.org/doc/draft-ietf-sidrops-aspa-verification/) -- Verification of AS_PATH Using ASPA Objects
- [draft-ietf-sidrops-8210bis](https://datatracker.ietf.org/doc/draft-ietf-sidrops-8210bis/) -- RTR v2 (adds ASPA support)
- [RFC 7947](https://datatracker.ietf.org/doc/html/rfc7947) -- Internet Exchange BGP Route Server
