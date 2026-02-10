# JSON Message Format

bgpipe can read and write BGP messages in a structured JSON format for processing, filtering, and archiving.
This page is a reference for the format, implemented by the [bgpfix library](https://github.com/bgpfix/bgpfix).

## Message Envelope

Each BGP message is a JSON array with up to 6 elements:

```json
["R", 243, "2025-07-11T11:23:50.860", "UPDATE", {...}, {...}]
```

| Index | Name   | Type       | Description                                                   |
|-------|--------|------------|---------------------------------------------------------------|
| `[0]` | `dir`  | string     | Direction: `"L"` (left-bound) or `"R"` (right-bound)         |
| `[1]` | `seq`  | integer    | Monotonically increasing sequence number                      |
| `[2]` | `time` | string     | Timestamp: `YYYY-MM-DDTHH:MM:SS.mmm` |
| `[3]` | `type` | string     | Message type (see table below)                                |
| `[4]` | `data` | object/string/null | Type-specific payload (see below)                    |
| `[5]` | `meta` | object/null | Message tags (key-value map)              |

### Message Types

| Type        | `data` field contains                              |
|-------------|-----------------------------------------------------|
| `OPEN`      | BGP session parameters and capabilities (object)    |
| `UPDATE`    | Prefixes and path attributes (object)               |
| `KEEPALIVE` | `null`                                              |

Unknown or unparsed types produce a hex string like `"0x1234abcd"`.

### Example

KEEPALIVE — the simplest message:
```json
["R", 2, "2025-07-11T08:47:22.659", "KEEPALIVE"]
```

## OPEN Messages

The `data` field contains BGP session parameters:

```json
{
  "bgp": 4,
  "asn": 65055,
  "id": "85.232.240.180",
  "hold": 7200,
  "caps": { ... }
}
```

| Field    | Type    | Description                                                  |
|----------|---------|--------------------------------------------------------------|
| `bgp`   | integer | BGP protocol version (always `4`)                            |
| `asn`   | integer | Autonomous System Number (2-byte in OPEN, see `AS4` cap)     |
| `id`    | string  | BGP Router ID in dotted-decimal notation                     |
| `hold`  | integer | Hold time in seconds                                         |
| `caps`  | object  | BGP capabilities (see below)                                 |
| `params`| string  | Raw optional parameters as hex (only when no capabilities)   |

### Capabilities

The `caps` object maps capability names to their values:

```json
{
  "MP": ["IPV4/UNICAST", "IPV6/UNICAST", "IPV4/FLOWSPEC"],
  "ROUTE_REFRESH": true,
  "EXTENDED_MESSAGE": true,
  "AS4": 65055,
  "ADDPATH": ["IPV4/UNICAST/SEND", "IPV6/UNICAST/BIDIR"],
  "ROLE": "CUSTOMER",
  "FQDN": {"host": "router1", "domain": "example.com"},
  "EXTENDED_NEXTHOP": ["IPV4/UNICAST/IPV6"]
}
```

| Capability             | Value type          | Description                                    |
|------------------------|---------------------|------------------------------------------------|
| `MP`                   | array of strings    | Multi-protocol AFI/SAFI list (RFC 4760)        |
| `ROUTE_REFRESH`        | `true`              | Route Refresh support (RFC 2918)               |
| `EXTENDED_MESSAGE`     | `true`              | Extended message support (RFC 8654)            |
| `AS4`                  | integer             | 4-byte ASN (RFC 6793)                          |
| `ADDPATH`              | array of strings    | ADD-PATH directions (RFC 7911): `AFI/SAFI/DIR` where DIR is `RECEIVE`, `SEND`, or `BIDIR` |
| `ROLE`                 | string              | BGP Role (RFC 9234): `PROVIDER`, `RS`, `RS-CLIENT`, `CUSTOMER`, or `PEER` |
| `FQDN`                | object              | Hostname capability (draft): `{"host": "...", "domain": "..."}` |
| `EXTENDED_NEXTHOP`     | array of strings    | Extended next-hop (RFC 8950): `AFI/SAFI/NH_AFI` |
| `GRACEFUL_RESTART`     | (presence/value)    | Graceful Restart (RFC 4724)                    |
| `ENHANCED_ROUTE_REFRESH` | `true`            | Enhanced Route Refresh (RFC 7313)              |
| `LLGR`                 | (presence/value)    | Long-Lived Graceful Restart                    |
| `PRE_ROUTE_REFRESH`    | `true`              | Pre-standard Route Refresh (code 128)          |

Unknown capabilities appear as `"CAP_N"` with a hex string value.

AFI/SAFI strings use the format `AFI/SAFI`, e.g.: `IPV4/UNICAST`, `IPV6/UNICAST`, `IPV4/MULTICAST`, `IPV6/MULTICAST`, `IPV4/FLOWSPEC`, `IPV6/FLOWSPEC`, `IPV4/MPLS_VPN`, etc.

### Complete OPEN Example

```json
[
  "L", 1, "2025-07-11T08:47:22.659", "OPEN",
  {
    "bgp": 4,
    "asn": 65055,
    "id": "85.232.240.180",
    "hold": 7200,
    "caps": {
      "MP": ["IPV4/FLOWSPEC"],
      "ROUTE_REFRESH": true,
      "EXTENDED_NEXTHOP": ["IPV4/UNICAST/IPV6", "IPV4/MULTICAST/IPV6"],
      "AS4": 65055,
      "PRE_ROUTE_REFRESH": true
    }
  },
  null
]
```

## UPDATE Messages

The `data` field contains prefixes and path attributes:

```json
{
  "reach": ["8.8.8.0/24", "8.8.4.0/24"],
  "unreach": ["192.0.2.0/24"],
  "attrs": { ... }
}
```

| Field     | Type   | Description                                                  |
|-----------|--------|--------------------------------------------------------------|
| `reach`   | array  | Announced IPv4 unicast prefixes (CIDR notation)              |
| `unreach` | array  | Withdrawn IPv4 unicast prefixes (CIDR notation)              |
| `attrs`   | object | Path attributes (see below)                                  |

For multi-protocol routes (IPv6, Flowspec, etc.), prefixes are in the `MP_REACH` and `MP_UNREACH` attributes instead.

### ADD-PATH

When ADD-PATH (RFC 7911) is negotiated, each prefix carries a 32-bit Path Identifier. In JSON, the path ID is prepended to the prefix string with `#` delimiters:

```json
"reach": ["#42#8.8.8.0/24", "#42#8.8.4.0/24"]
```

The format is `#<path-id>#<prefix>`. Without ADD-PATH, prefixes appear as plain CIDR strings.

### Path Attributes

Each attribute in `attrs` is an object with `flags` and `value`:

```json
"ORIGIN": {
  "flags": "T",
  "value": "IGP"
}
```

**Flags** are a string of characters:

| Flag | Meaning          |
|------|------------------|
| `O`  | Optional         |
| `T`  | Transitive       |
| `P`  | Partial          |
| `X`  | Extended length  |

### Attribute Reference

| Attribute          | Flags | Value format                           | Description                     |
|--------------------|-------|----------------------------------------|---------------------------------|
| `ORIGIN`           | `T`   | `"IGP"`, `"EGP"`, or `"INCOMPLETE"`   | Route origin (RFC 4271)         |
| `ASPATH`           | `T`   | array of integers / nested arrays      | AS path (see below)             |
| `NEXTHOP`          | `T`   | `"192.0.2.1"`                          | IPv4 next-hop address           |
| `MED`              | `O`   | integer                                | Multi-Exit Discriminator        |
| `LOCALPREF`        | `T`   | integer                                | Local Preference                |
| `AGGREGATE`        | `T`   | *(empty/presence)*                     | Atomic Aggregate marker         |
| `AGGREGATOR`       | `OT`  | `{"asn": N, "addr": "IP"}`            | Aggregator (RFC 4271)           |
| `COMMUNITY`        | `OT`  | `["65000:100", "65000:200"]`           | Standard communities (RFC 1997) |
| `ORIGINATOR`       | `O`   | `"192.0.2.1"`                          | Route Reflector originator      |
| `CLUSTER_LIST`     | `O`   | `["192.0.2.1", "192.0.2.2"]`          | Route Reflector cluster list    |
| `MP_REACH`         | `OX`  | object (see MP-BGP section)            | Multi-protocol NLRI             |
| `MP_UNREACH`       | `OX`  | object (see MP-BGP section)            | Multi-protocol withdrawals      |
| `EXT_COMMUNITY`    | `OT`  | array of objects (see below)           | Extended communities (RFC 4360) |
| `AS4PATH`          | `OT`  | same format as `ASPATH`                | 4-byte AS path (RFC 6793)       |
| `AS4AGGREGATOR`    | `OT`  | same format as `AGGREGATOR`            | 4-byte Aggregator (RFC 6793)    |
| `OTC`              | `OT`  | integer                                | Only-To-Customer (RFC 9234)     |
| `LARGE_COMMUNITY`  | `OT`  | `["65000:100:1", "65000:200:2"]`       | Large communities (RFC 8092)    |

Unrecognized attributes appear as `ATTR_N` with a hex string value.

### ASPATH

AS_SEQUENCE segments are flat; AS_SET segments are nested arrays:

```json
"ASPATH": {"flags": "T", "value": [64515, 20473, 15169]}
```

With an AS_SET:

```json
"ASPATH": {"flags": "T", "value": [64515, [20473, 15169]]}
```

The AS_SET `[20473, 15169]` is a single hop containing multiple ASNs. AS_CONFED_SEQUENCE and AS_CONFED_SET use the same representation (flat/nested) with no distinct marker.

### Communities

**Standard** (`COMMUNITY`) — array of `"ASN:VALUE"` strings (both uint16):

```json
"COMMUNITY": {"flags": "OT", "value": ["64515:100", "8218:20000"]}
```

**Large** (`LARGE_COMMUNITY`) — array of `"ASN:VALUE1:VALUE2"` strings (all uint32):

```json
"LARGE_COMMUNITY": {"flags": "OT", "value": ["20473:300:15169"]}
```

**Extended** (`EXT_COMMUNITY`) — array of objects with `type`, `value`, and optional `nontransitive`:

```json
"EXT_COMMUNITY": {
  "flags": "OT",
  "value": [
    {"type": "TARGET", "value": "65000:100"},
    {"type": "IP4_TARGET", "value": "192.0.2.1:100"},
    {"type": "AS4_TARGET", "value": "65000:100"},
    {"type": "ORIGIN", "value": "65000:200"}
  ]
}
```

Extended community type names:

| Type name           | Description                          | Value format          |
|---------------------|--------------------------------------|-----------------------|
| `TARGET`            | Route Target (2-byte ASN)            | `"ASN:value"`         |
| `ORIGIN`            | Route Origin (2-byte ASN)            | `"ASN:value"`         |
| `IP4_TARGET`        | Route Target (IPv4)                  | `"IP:value"`          |
| `IP4_ORIGIN`        | Route Origin (IPv4)                  | `"IP:value"`          |
| `AS4_TARGET`        | Route Target (4-byte ASN)            | `"ASN:value"`         |
| `AS4_ORIGIN`        | Route Origin (4-byte ASN)            | `"ASN:value"`         |

Unknown types appear as `"0xNNNN"`. The `"nontransitive": true` field is added when the community is non-transitive across ASes.

For Flowspec-specific extended community types (`FLOW_RATE_BYTES`, `FLOW_REDIRECT_AS2`, etc.), see [Flowspec](flowspec.md).

## MP-BGP (Multi-Protocol)

### MP_REACH

For IPv6 and other multi-protocol reachable NLRI:

```json
"MP_REACH": {
  "flags": "OX",
  "value": {
    "af": "IPV6/UNICAST",
    "nexthop": "2001:db8::1",
    "prefixes": ["2001:db8:1::/48", "2001:db8:2::/48"]
  }
}
```

| Field       | Type          | Description                                     |
|-------------|---------------|-------------------------------------------------|
| `af`        | string        | Address family: `"AFI/SAFI"`                    |
| `nexthop`   | string        | Next-hop address (omitted when unspecified)      |
| `link-local`| string        | IPv6 link-local next-hop (optional, IPv6 only)  |
| `prefixes`  | array         | Prefix strings in CIDR notation                 |

For Flowspec address families, the format uses `rules` instead of `prefixes` — see [Flowspec](flowspec.md).

When the NLRI is not parsed (unsupported address family), the value falls back to:

```json
{"af": "AFI/SAFI", "nh": "0x...", "data": "0x..."}
```

### MP_UNREACH

Same structure without `nexthop`:

```json
"MP_UNREACH": {
  "flags": "OX",
  "value": {
    "af": "IPV6/UNICAST",
    "prefixes": ["2001:db8:3::/48"]
  }
}
```

## Complete UPDATE Example

```json
[
  "R", 243, "2025-07-11T11:23:50.860", "UPDATE",
  {
    "reach": ["8.8.8.0/24", "8.8.4.0/24"],
    "attrs": {
      "ORIGIN": {"flags": "T", "value": "IGP"},
      "ASPATH": {"flags": "T", "value": [64515, 15169]},
      "NEXTHOP": {"flags": "T", "value": "192.0.2.1"},
      "COMMUNITY": {"flags": "OT", "value": ["64515:100"]},
      "OTC": {"flags": "OTP", "value": 6777}
    }
  },
  {"PEER_AS": "8218", "PEER_IP": "5.57.80.210"}
]
```

## Implementation Notes

- **Hex fallback**: Unparsed upper layers and attributes appear as hex strings like `"0x1234abcd"`.
- **Bidirectional**: The format fully supports both encoding (JSON to BGP wire) and decoding (BGP wire to JSON).
- **Timestamps**: Millisecond precision, format `YYYY-MM-DDTHH:MM:SS.mmm`.
- **Prefixes**: Standard CIDR notation (`192.0.2.0/24`, `2001:db8::/32`).
- **Metadata** (`[5]`): Contains pipeline tags (e.g., from the `tag` stage, `ris-live` peer info, `rpki` validation status). Can be `null` when no metadata is attached.

## See Also

- [Flowspec Format](flowspec.md) — Flowspec rules and actions in JSON
- [Message Filters](filters.md) — Filter BGP messages using `grep` and `drop` stages
- [Examples](examples.md) — Practical bgpipe command-line examples
- [bgpfix library](https://github.com/bgpfix/bgpfix) — The underlying Go library
