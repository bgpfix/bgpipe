# aspa

Validate AS paths using RPKI ASPA.

## Synopsis

```
bgpipe [--rpki SOURCE] [...] -- aspa [OPTIONS]
```

## Description

The **aspa** stage performs ASPA verification
([draft-ietf-sidrops-aspa-verification](https://datatracker.ietf.org/doc/draft-ietf-sidrops-aspa-verification/))
of BGP UPDATE messages: it detects route leaks by verifying that the AS_PATH
is valley-free, using ASPA records that attest provider-customer relationships
between ASes. Each path is assigned one of three states:

- **VALID** -- the path is valley-free with full cryptographic attestation
- **UNKNOWN** -- the path appears valley-free but some ASes lack ASPA records
  (insufficient attestation, not evidence of a leak)
- **INVALID** -- the path provably violates valley-free routing (route leak)

ASPA records come from the shared RPKI cache, maintained by the bgpipe core
and configured with the global `--rpki` options -- see
[RPKI Data Source](rov.md#rpki-data-source) for details. Note that ASPA
records require an RTR v2 server
([draft-ietf-sidrops-8210bis](https://datatracker.ietf.org/doc/draft-ietf-sidrops-8210bis/))
or a JSON file with ASPA data.

ASPA verification also requires knowledge of the peer's BGP role, either
auto-detected via the [RFC 9234](https://datatracker.ietf.org/doc/html/rfc9234)
BGP Role capability, or set explicitly with `--role`. If `--role auto` (the
default) and the peer does not send the BGP Role capability in their OPEN
message, ASPA validation is skipped for the session and a warning is logged.

The stage also verifies that the first AS in the path matches the neighbor's
ASN (per draft-ietf-sidrops-aspa-verification, Section 5). This check is
skipped for Route Server peers (`--role rs`), as RSes do not prepend their ASN
([RFC 7947](https://datatracker.ietf.org/doc/html/rfc7947)), and can be
disabled entirely with `--first-hop=false` -- see
[Multi-peer feeds](#multi-peer-feeds).

### Actions for INVALID paths

ASPA validates the entire AS_PATH (one per UPDATE), so the `--invalid` action
applies to the whole message:

| Action | Behavior |
|--------|----------|
| `withdraw` | Move all reachable prefixes to withdrawn |
| `drop` | Drop the entire UPDATE message |
| `keep` | Keep unchanged (tag only) |

### Tags

When `--tag` is enabled (default), the message gets an `aspa/status` tag.
For INVALID paths, the failing hop is reported in `aspa/invalid-hop` as
`"<customer-asn> <provider-asn>"`. These can be used in downstream
[filters](../filters.md).

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--invalid` | string | `withdraw` | Action for INVALID paths: `withdraw`, `drop`, `keep` |
| `--tag` | bool | `true` | Add `aspa/status` and `aspa/invalid-hop` tags |
| `--event` | string | | Emit named event on INVALID paths |
| `--role` | string | `auto` | Peer's BGP role: `auto`, `provider`, `customer`, `peer`, `rs`, `rs-client` |
| `--peer-tag` | string | | Read the peer ASN from given message tag (eg. `PEER_AS`) instead of OPEN |
| `--first-hop` | bool | `true` | Check that `path[0]` equals the neighbor ASN; disable for collector feeds ([RFC 7947](https://datatracker.ietf.org/doc/html/rfc7947)) |
| `--no-wait` | bool | `false` | Start before the RPKI cache is ready |

### Multi-peer feeds

Route collector feeds like [ris-live](ris-live.md) and [rv-live](rv-live.md)
aggregate sessions from many different peers in a single message stream, so
there is no single OPEN message to take the neighbor ASN (or BGP role) from.
Use `--peer-tag PEER_AS` to read the neighbor ASN per-message from the tag
that these stages attach, which enables the first-hop check for every peer
individually. `--peer-tag` requires an explicit `--role`, which then applies
to all peers in the stream. If a message lacks the tag, the first-hop check
is skipped for that message.

On a collector feed the first-hop check is often more noise than signal: the
collector's `PEER_AS` is not a real routing adjacency, and IXP route servers
in the feed do not prepend their ASN ([RFC 7947](https://datatracker.ietf.org/doc/html/rfc7947)),
so a legitimate path whose `path[0]` is the route server's client is flagged
INVALID. Pass `--first-hop=false` to drop the check entirely and validate paths
only on their merits (the valley-free / attestation checks), which is the
appropriate posture for passive monitoring of a multiplexed collector feed.

The `--role` option specifies the peer's BGP role (from the peer's
perspective, per [RFC 9234](https://datatracker.ietf.org/doc/html/rfc9234)):

| `--role` value | Meaning | ASPA direction |
|---------------|---------|----------------|
| `provider` | Peer is our provider | Downstream (route came from above) |
| `rs` | Peer is a Route Server | Upstream (first-hop ASN check is skipped) |
| `customer` | Peer is our customer | Upstream (route came from below) |
| `rs-client` | Peer is an RS client | Upstream |
| `peer` | Peer is a lateral peer | Upstream |
| `auto` | Auto-detect from BGP Role capability | Depends on detected role |

In bidirectional (`-LR`) mode only `--role auto` is allowed: a single explicit
role cannot describe two different peers.

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `bgpipe_aspa_messages_total` | counter | UPDATE messages processed |
| `bgpipe_aspa_valid_total` | counter | Paths with ASPA state VALID |
| `bgpipe_aspa_unknown_total` | counter | Paths with ASPA state UNKNOWN |
| `bgpipe_aspa_invalid_total` | counter | Paths with ASPA state INVALID |
| `bgpipe_rpki_aspa_entries` | gauge | ASPA records loaded in the shared cache |

## Examples

ROV + ASPA with explicit peer role:

```bash
bgpipe \
    -- listen :179 \
    -- rov \
    -- aspa --role customer \
    -- connect 192.0.2.1
```

ASPA route-leak monitoring on a live route collector feed, with current RPKI
data fetched from the rpki-client console (tag everything, enforce nothing):

```bash
bgpipe -g --rpki https://console.rpki-client.org/vrps.json \
    -- ris-live \
    -- aspa --role provider --peer-tag PEER_AS --invalid keep \
    -- grep 'tag[aspa/status] == INVALID' \
    -- write invalid-paths.json
```

Use a local JSON file with ASPA records:

```bash
bgpipe --rpki /var/lib/rpki/export.json \
    -- listen :179 \
    -- aspa --role customer --invalid drop \
    -- connect 192.0.2.1
```

Emit an event for detected route leaks:

```bash
bgpipe --events aspa/leak \
    -- listen :179 \
    -- aspa --role customer --invalid keep --event leak \
    -- connect 192.0.2.1
```

## See Also

[rov](rov.md),
[grep](grep.md),
[update](update.md),
[Stages overview](index.md)

### References

- [draft-ietf-sidrops-aspa-verification](https://datatracker.ietf.org/doc/draft-ietf-sidrops-aspa-verification/) -- Verification of AS_PATH Using ASPA Objects
- [draft-ietf-sidrops-8210bis](https://datatracker.ietf.org/doc/draft-ietf-sidrops-8210bis/) -- RTR v2 (adds ASPA support)
- [RFC 9234](https://datatracker.ietf.org/doc/html/rfc9234) -- Route Leak Prevention and Detection Using Roles in UPDATE and OPEN Messages
- [RFC 7947](https://datatracker.ietf.org/doc/html/rfc7947) -- Internet Exchange BGP Route Server
