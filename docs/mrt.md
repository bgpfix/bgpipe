# MRT Files

MRT ([RFC 6396](https://datatracker.ietf.org/doc/html/rfc6396)) is the standard
archive format for BGP data. Public collectors such as
[RouteViews](https://archive.routeviews.org/) and
[RIPE NCC RIS](https://ris.ripe.net/docs/mrt/) publish two kinds of MRT files:

- **update dumps** -- a log of BGP UPDATE messages as they arrived, usually
  covering a 5 or 15 minute window (`updates.*`)
- **table dumps** -- a full RIB snapshot taken every few hours, listing every
  prefix each peer had in its table at that moment (`rib.*`, `bview.*`)

bgpipe reads both through the [read](stages/read.md) stage and turns them into
the same BGP message stream, so every downstream stage -- [grep](stages/grep.md),
[rov](stages/rov.md), [aspa](stages/aspa.md), [write](stages/write.md) -- works
identically on either.

## Supported record types

| MRT type | Subtypes | Source | Support |
|----------|----------|--------|---------|
| `BGP4MP` / `BGP4MP_ET` (16, 17) | `MESSAGE`, `MESSAGE_AS4`, `*_LOCAL`, `*_ADDPATH` | update dumps | read + write |
| `TABLE_DUMP_V2` (13) | `PEER_INDEX_TABLE`, `RIB_IPV4_UNICAST`, `RIB_IPV6_UNICAST` | modern table dumps | read |
| `TABLE_DUMP` (12) | `AFI_IPV4`, `AFI_IPV6` | legacy table dumps (older archives) | read |

Records of other types and subtypes (`RIB_GENERIC`, multicast RIBs, ADD-PATH
RIBs, OSPF, IS-IS) are skipped silently, so a mixed archive still streams the
records bgpipe understands.

Writing MRT (via [write](stages/write.md) `--format mrt`) always produces
`BGP4MP_ET` update records; table dumps are read-only.

## Update dumps

Update dumps contain complete BGP messages, so they are replayed as-is. Each
message carries the session metadata from the MRT record as tags:

| Tag | Description |
|-----|-------------|
| `PEER_AS` | AS number of the peer the message came from |
| `PEER_IP` | IP address of that peer |
| `LOCAL_AS` | AS number of the collector |
| `LOCAL_IP` | collector IP address for this session |
| `INTERFACE` | interface index, when present |

The record subtype determines how the embedded message is decoded: AS4 subtypes
force 4-byte ASN parsing, ADD-PATH subtypes force Path Identifier parsing. This
overrides any session capabilities, so archives decode correctly regardless of
what the rest of the pipeline negotiated.

## Table dumps

A table dump is not a message log -- it is a snapshot of routing state. Each
record describes one prefix, followed by one entry per peer that had a route to
it, each with that peer's path attributes.

bgpipe converts these into **synthetic UPDATE messages**: the prefix becomes
NLRI, the entry's attributes become the message attributes, and the peer is
recorded in tags. The result is a stream that looks exactly like a router
receiving the full table from every peer at once.

### Peer tags

Every message from a table dump is tagged with its origin peer:

| Tag | Description |
|-----|-------------|
| `PEER_AS` | AS number of the peer this route came from |
| `PEER_IP` | IP address of that peer |

Collectors typically hold 20-50 peers, and a full IPv4 table has around a
million prefixes, so a single RIB dump expands to tens of millions of routes.
All peers are streamed by default; select one with a
[tag filter](filters.md#tags) (see [use cases](#use-cases) below).

### Prefix bundling

Table dump records are sorted by prefix, so neighbouring prefixes from the same
peer very often share identical attributes -- a covering prefix and its
more-specifics, for instance. bgpipe bundles such runs into a single UPDATE with
multiple NLRI, exactly as a real router would pack them:

```json
["R",29,"2026-07-23T06:00:00.000","UPDATE",{"reach":["1.0.4.0/24","1.0.5.0/24","1.0.6.0/24","1.0.7.0/24"],"attrs":{"ORIGIN":{"flags":"T","value":"IGP"},"ASPATH":{"flags":"TX","value":[2497,4826,2764,38803]},"NEXTHOP":{"flags":"T","value":"202.232.0.3"}}},{"PEER_AS":"2497","PEER_IP":"202.232.0.3"}]
```

Bundling is per peer and stops as soon as the attributes change, so no
information is lost -- it only reduces message count, typically by a factor of
2-3. IPv6 works the same way, with prefixes and next hop carried in `MP_REACH`:

```json
["R",201,"2026-07-23T06:00:00.000","UPDATE",{"attrs":{"ORIGIN":{"flags":"T","value":"IGP"},"ASPATH":{"flags":"TX","value":[2497,2914]},"MP_REACH":{"flags":"O","value":{"af":"IPV6/UNICAST","nexthop":"2001:240:100:ff::2497:2","prefixes":["2001:218:8000::/38","2001:218:e000::/38"]}}}},{"PEER_AS":"2497","PEER_IP":"2001:240:100:ff::2497:2"}]
```

If you need strictly one prefix per record for analysis, expand the bundles
downstream, e.g. with `jq`:

```bash
bgpipe -- read rib.20260723.0600.bz2 \
  | jq -c '.[4] as $u | .[5] as $t
           | ($u.reach // []) + ($u.attrs.MP_REACH.value.prefixes // [])
           | .[] | {prefix: ., peer: $t.PEER_AS, path: $u.attrs.ASPATH.value}'
```

## Getting the files

Table dumps follow predictable naming, which bgpipe uses for
[format detection](#format-detection):

| Source | URL pattern |
|--------|-------------|
| RouteViews (IPv4 + IPv6) | `https://archive.routeviews.org/bgpdata/YYYY.MM/RIBS/rib.YYYYMMDD.HHMM.bz2` |
| RouteViews IPv6 collector | `https://archive.routeviews.org/route-views6/bgpdata/YYYY.MM/RIBS/rib.YYYYMMDD.HHMM.bz2` |
| RIPE RIS (latest) | `https://data.ris.ripe.net/rrcNN/latest-bview.gz` |
| RIPE RIS (archive) | `https://data.ris.ripe.net/rrcNN/YYYY.MM/bview.YYYYMMDD.HHMM.gz` |

Files are streamed and decompressed on the fly, so you can read a URL directly
without downloading it first:

```bash
bgpipe -o -- read https://data.ris.ripe.net/rrc01/latest-bview.gz
```

RIB dumps are large -- a compressed RouteViews IPv4 RIB is around 70 MB, a RIPE
RIS `latest-bview.gz` several hundred MB -- but bgpipe never holds the table in
memory. It streams record by record, keeping only one pending message per peer.

## Format detection

By default `--format auto` detects MRT from the file name:

- `updates.2*`, `latest-update.gz` -- update dumps
- `rib.2*`, `bview.2*`, `latest-bview.gz` -- table dumps
- any `.mrt` extension (after stripping `.gz`, `.bz2`, `.zst`)

Compression suffixes are handled separately by `--decompress` and do not
interfere with detection.

!!! warning "Non-standard file names"

    If name detection fails, bgpipe falls back to sampling the file contents --
    but that check looks for a BGP message marker, which **table dumps do not
    contain**. A RIB snapshot under an unrecognised name (or arriving on stdin)
    must be given `--format mrt` explicitly:

    ```bash
    bgpipe -- read --format mrt my-snapshot.dat -- write out.json
    cat rib.dat | bgpipe -- read --format mrt - -- write out.json
    ```

## Use cases

### Validate a full routing table with RPKI

Check an entire RIB snapshot against RPKI and keep only the invalid routes.
`--invalid keep` leaves messages in the stream so they can be tagged and
inspected rather than withdrawn:

```bash
bgpipe --rpki https://rpki.example.net/rpki.json \
  -- read https://archive.routeviews.org/bgpdata/2026.07/RIBS/rib.20260723.0600.bz2 \
  -- rov --invalid keep \
  -- grep 'tag[rov/status] == INVALID' \
  -- write invalid-routes.json
```

This answers "what was RPKI-invalid in the DFZ at 06:00 that day", per peer.
Swap [rov](stages/rov.md) for [aspa](stages/aspa.md) to hunt for route leaks
instead.

### Extract a single peer's view

A collector RIB holds every peer's table. Select one peer to get exactly the
routes that peer advertised:

```bash
bgpipe -- read rib.20260723.0600.bz2 \
  -- grep 'tag[PEER_AS] == "6939"' \
  -- write as6939-table.json
```

Filtering on `PEER_IP` works the same way and is more precise when a peer has
multiple sessions with the collector.

### Look up a prefix across all peers

See how a prefix was routed from every vantage point at snapshot time:

```bash
bgpipe -o -- read rib.20260723.0600.bz2 \
  -- grep 'prefix ~ 8.0.0.0/8'
```

Each matching message shows one peer's AS path and next hop for that prefix.

### Load a real table into a router or lab

Replay a RIB snapshot into a live BGP session -- useful for filling a test
router with a realistic table, benchmarking, or reproducing an incident:

```bash
bgpipe \
  -- speaker --active --asn 65055 \
  -- read --wait ESTABLISHED rib.20260723.0600.bz2 \
  -- listen :179
```

Restrict it to a single peer's view (as above) to load one full table rather
than every peer's copy.

### Convert to JSON for analysis

```bash
bgpipe -- read rib.20260723.0600.bz2 -- write --type UPDATE rib.json.gz
```

Output compression is chosen from the file name, so large snapshots stay
manageable. Combine with [grep](stages/grep.md) or `--type` to keep only what
you need.

## Limitations

- Table dumps are **read-only**; bgpipe writes update dumps (`BGP4MP_ET`) only.
- `RIB_GENERIC`, multicast RIBs, and ADD-PATH RIB subtypes are skipped.
- Synthetic UPDATEs carry no withdrawals -- a snapshot only states what was
  present, so [update](stages/update.md)-style before/after comparisons need two
  snapshots diffed downstream.
- Timestamps come from the MRT record header, so every message from one snapshot
  shares the snapshot time; the per-route "originated" time in RIB entries is not
  exposed as a tag.

## See Also

[read](stages/read.md),
[write](stages/write.md),
[Message filters](filters.md),
[JSON format](json-format.md),
[Examples](examples.md)
