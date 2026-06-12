# rov

Validate route origins using RPKI ROV.

## Synopsis

```
bgpipe [--rpki SOURCE] [...] -- rov [OPTIONS]
```

## Description

The **rov** stage performs RPKI Route Origin Validation
([RFC 6811](https://datatracker.ietf.org/doc/html/rfc6811)) of BGP UPDATE
messages: it checks whether the origin AS is authorized to announce each
prefix, using VRPs (Validated ROA Payloads) from the shared
[RPKI cache](#rpki-data-source). Each prefix is assigned one of three states:

- **VALID** -- a VRP covers this prefix with a matching origin AS and maxLength
- **INVALID** -- a VRP exists for the prefix but the origin AS or length does not match
- **NOT_FOUND** -- no VRP covers this prefix

ROV in this stage assumes the origin ASN comes from the AS_PATH, so it is aimed
at eBGP edges and route-server feeds. Empty AS_PATH updates, common on iBGP or
locally originated routes, are out of scope here.

With `--strict`, NOT_FOUND prefixes are treated the same as INVALID.

For AS_PATH validation against route leaks, see the [aspa](aspa.md) stage.

### RPKI Data Source

The RPKI cache is shared by all `rov` and `aspa` stages in the pipeline, and
maintained by the bgpipe core. It is configured with global options, specified
before the first stage:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--rpki` | string | `rtr.rpki.cloudflare.com:8282` | RPKI cache source: RTR `host:port`, `tls://host:port`, an HTTP(S) URL, or a local file path |
| `--rpki-refresh` | duration | `1h` | RTR refresh interval (periodic Serial Query), or URL re-fetch interval |
| `--rpki-retry` | duration | `10m` | RTR reconnect interval after failure |
| `--rpki-insecure` | bool | `false` | Do not validate the RTR server TLS certificate |

The RTR client supports protocol versions 0/1/2 with automatic version
negotiation and fallback. ASPA records require an RTR v2 server
([draft-ietf-sidrops-8210bis](https://datatracker.ietf.org/doc/draft-ietf-sidrops-8210bis/)).

If `--rpki` is an `http://` or `https://` URL, the JSON/CSV data is fetched
from it and re-fetched every `--rpki-refresh`. For example,
`--rpki https://console.rpki-client.org/vrps.json` uses the public
[rpki-client console](https://console.rpki-client.org/) export, which
includes both VRPs and ASPAs.

If `--rpki` points to an existing local file, it is loaded instead and
auto-reloaded every 10 seconds. JSON files support VRPs and ASPAs; CSV
supports VRPs only.

JSON format (Routinator and rpki-client compatible):

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

### Actions for INVALID prefixes

The `--invalid` option controls handling, per-prefix:

| Action | Behavior |
|--------|----------|
| `withdraw` | Move invalid prefixes to withdrawn |
| `filter` | Remove invalid prefixes silently |
| `drop` | Drop the entire UPDATE message |
| `split` | Split invalid prefixes to a separate UPDATE with withdrawals |
| `keep` | Keep unchanged (tag only) |

### Tags

When `--tag` is enabled (default), each prefix gets a per-prefix tag
(`rov/<prefix>`) and the message gets an overall `rov/status` tag.
These can be used in downstream [filters](../filters.md).

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--invalid` | string | `withdraw` | Action for INVALID prefixes: `withdraw`, `filter`, `drop`, `split`, `keep` |
| `--strict` | bool | `false` | Treat NOT_FOUND as INVALID |
| `--tag` | bool | `true` | Add `rov/status` and `rov/<prefix>` tags |
| `--event` | string | | Emit named event on INVALID prefixes |
| `--no-wait` | bool | `false` | Start before the RPKI cache is ready |

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `bgpipe_rov_messages_total` | counter | UPDATE messages processed |
| `bgpipe_rov_valid_total` | counter | Prefixes with ROV state VALID |
| `bgpipe_rov_invalid_total` | counter | Prefixes with ROV state INVALID |
| `bgpipe_rov_not_found_total` | counter | Prefixes with ROV state NOT_FOUND |
| `bgpipe_rpki_vrps_ipv4` | gauge | IPv4 VRPs loaded in the shared cache |
| `bgpipe_rpki_vrps_ipv6` | gauge | IPv6 VRPs loaded in the shared cache |

## Examples

Basic ROV (default: withdraw INVALID prefixes):

```bash
bgpipe \
    -- listen :179 \
    -- rov \
    -- connect 192.0.2.1
```

Tag only, no enforcement -- useful for monitoring:

```bash
bgpipe -o \
    -- ris-live \
    -- rov --invalid keep \
    -- grep 'tag[rov/status] == INVALID'
```

Strict ROV: drop any prefix without a valid VRP:

```bash
bgpipe --events rov/dropped \
    -- listen :179 \
    -- rov --strict --invalid drop --event dropped \
    -- connect 192.0.2.1
```

Use a local JSON or CSV file instead of RTR:

```bash
bgpipe --rpki /var/lib/rpki/export.json \
    -- listen :179 \
    -- rov --invalid filter \
    -- connect 192.0.2.1
```

Add a community to invalid routes instead of dropping:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- rov --invalid keep \
    -- update --if 'tag[rov/status] == INVALID' --add-com 65000:666 \
    -- connect 10.0.0.1
```

Connect to a private RTR cache over TLS:

```bash
bgpipe --rpki tls://rpki.example.com:8323 \
    -- listen :179 \
    -- rov \
    -- connect 192.0.2.1
```

## See Also

[aspa](aspa.md),
[limit](limit.md),
[grep](grep.md),
[update](update.md),
[Stages overview](index.md)

### References

- [RFC 6811](https://datatracker.ietf.org/doc/html/rfc6811) -- RPKI-Based Route Origin Validation
- [RFC 8210](https://datatracker.ietf.org/doc/html/rfc8210) -- RPKI-to-Router Protocol (RTR v1)
- [draft-ietf-sidrops-8210bis](https://datatracker.ietf.org/doc/draft-ietf-sidrops-8210bis/) -- RTR v2 (adds ASPA support)
