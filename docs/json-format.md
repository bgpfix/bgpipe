# JSON Message Format

bgpipe can represent BGP messages in a structured JSON format for easy processing and filtering.
This page documents the format, which is implemented by the [bgpfix library](https://github.com/bgpfix/bgpfix). For example, the JSON translation feature can be useful for external processors - e.g. Python scripts via the `exec` or `websocket` stages - or for BGP message inspection or archiving.

## Overview

Each BGP message is represented as a JSON array with the following structure:

```json
[dir, seq, time, type, data, meta]
```

Where:

| Index | Name | Type | Description |
|-------|------|------|-------------|
| `[0]` | `dir` | string | direction: `L` (left) or `R` (right) |
| `[1]` | `seq` | int | sequence number (monotonic counter) |
| `[2]` | `time` | string | timestamp in `YYYY-MM-DDTHH:MM:SS.mmm` format |
| `[3]` | `type` | string/int | type: `OPEN`, `UPDATE`, `KEEPALIVE`, etc. |
| `[4]` | `data` | object/string | type-specific object or raw hex string (optional) |
| `[5]` | `meta` | object | bgpipe metadata (optional) |

Example KEEPALIVE Message

```json
["R", 2, "2025-07-11T08:47:22.659", "KEEPALIVE"]
```

## OPEN Messages

For OPEN messages, the `data` field contains a JSON object with the BGP session parameters and capabilities:

```json
{
  "bgp": 4,
  "asn": 65055,
  "id": "192.0.2.1",
  "hold": 90,
  "caps": { ... } // or "params": "0x..." if no caps
}
```

Where:

| Field | Type | Description |
|-------|------|-------------|
| `bgp` | int | BGP protocol version (always 4) |
| `asn` | int | Autonomous System Number (2-byte) |
| `id` | string | BGP Router ID in dotted-decimal IPv4 format |
| `hold` | int | Hold time in seconds |
| `caps` | object | BGP capabilities (see below) |
| `params` | string | Raw optional parameters as hex string (only if no caps) |

The `caps` object contains BGP capabilities:

```json
{
  "MP": ["IPV4/UNICAST"],
  "ROUTE_REFRESH": true,
  "EXTENDED_MESSAGE": true,
  "AS4": 64515
}
```

Common capabilities (see [bgpfix source](https://github.com/bgpfix/bgpfix/blob/main/caps/cap.go)):

- **`MP`**: Array of supported AFI/SAFI combinations (e.g., `IPV4/UNICAST`, `IPV6/FLOWSPEC`)
- **`ROUTE_REFRESH`**: Boolean, supports Route Refresh (RFC 2918)
- **`EXTENDED_MESSAGE`**: Boolean, supports messages larger than 4096 bytes (RFC 8654)
- **`AS4`**: Number, 4-byte ASN value (RFC 6793)

Complete example:

```json
[
  "L",
  1,
  "2025-07-11T08:47:22.659",
  "OPEN",
  {
    "bgp": 4,
    "asn": 65055,
    "id": "85.232.240.180",
    "hold": 7200,
    "caps": {
      "MP": ["IPV4/FLOWSPEC"],
      "ROUTE_REFRESH": true
    }
  }
]
```

## UPDATE Messages

For UPDATE messages, element `data` field contains a JSON object with reachable/withdrawn IPv4 prefixes and the path attributes:

```json
{
  "reach": [ ... ],
  "unreach": [ ... ],
  "attrs": { ... }
}
```

Where:

| Field | Type | Description |
|-------|------|-------------|
| `reach` | array | Array of reachable IPv4 unicast prefixes (announced routes) |
| `unreach` | array | Array of unreachable IPv4 unicast prefixes (withdrawn routes) |
| `attrs` | object | BGP path attributes (see below) |

**Note**: For MP-BGP (multi-protocol) routes (IPv6, Flowspec, etc.), prefixes are embedded within the `MP_REACH` or `MP_UNREACH` attributes.

### Path Attributes

The `attrs` object contains BGP path attributes. Each attribute has:

- **Key**: Attribute name (e.g., `ORIGIN`, `ASPATH`, `COMMUNITY`)
- **Value**: Object with `flags` and `value` fields

```json
"ORIGIN": {
  "flags": "T",
  "value": "IGP"
}
```

#### Attribute Flags

Flags are a string combining these characters:

- **`O`**: Optional
- **`T`**: Transitive
- **`P`**: Partial
- **`X`**: Extended length

Well-known attributes (ORIGIN, ASPATH, NEXTHOP) use `T` only. Optional attributes use combinations like `OT`.

### Common Path Attributes

#### ORIGIN

Origin of the route:

```json
"ORIGIN": {
  "flags": "T",
  "value": "IGP"  // or EGP or INCOMPLETE
}
```

#### ASPATH

AS path as an array. AS sequences are flat arrays, AS sets are nested arrays:

```json
"ASPATH": {
  "flags": "T",
  "value": [64515, 20473, 15169]  // AS_SEQUENCE
}
```

With AS_SET:

```json
"ASPATH": {
  "flags": "T",
  "value": [64515, [20473, 15169]]  // last is AS_SET
}
```

#### NEXTHOP

IPv4 next-hop address:

```json
"NEXTHOP": {
  "flags": "T",
  "value": "192.0.2.1"
}
```

#### MED (Multi-Exit Discriminator)

```json
"MED": {
  "flags": "O",
  "value": 100
}
```

#### LOCALPREF (Local Preference)

```json
"LOCALPREF": {
  "flags": "T",
  "value": 200
}
```

#### COMMUNITY

Standard BGP communities as `ASN:value` strings:

```json
"COMMUNITY": {
  "flags": "OT",
  "value": ["65000:100", "65000:200"]
}
```

#### LARGE_COMMUNITY

Large BGP communities (RFC 8092):

```json
"LARGE_COMMUNITY": {
  "flags": "OT",
  "value": ["65000:100:1", "65000:200:2"]
}
```

#### EXT_COMMUNITY (Extended Communities)

Extended communities with type-specific encoding:

```json
"EXT_COMMUNITY": {
  "flags": "OT",
  "value": [
    {"type": "RT", "asn": 65000, "val": 100},
    {"type": "RT", "ip": "192.0.2.1", "val": 100}
  ]
}
```

Common types: `RT` (Route Target), `RO` (Route Origin), `REDIR_IP4` (Flowspec redirect), `RATE` (Flowspec rate-limit).

### MP-BGP Attributes

#### MP_REACH

Multi-protocol reachable NLRI (used for IPv6, Flowspec, etc.):

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

For Flowspec, see the Flowspec section below.

#### MP_UNREACH

Multi-protocol unreachable NLRI (withdrawals):

```json
"MP_UNREACH": {
  "flags": "OX",
  "value": {
    "af": "IPV6/UNICAST",
    "prefixes": ["2001:db8:3::/48"]
  }
}
```

### Complete UPDATE Example

```json
[
  "R",
  243,
  "2025-07-11T11:23:50.860",
  "UPDATE",
  {
    "reach": ["8.8.8.0/24", "8.8.4.0/24"],
    "attrs": {
      "ORIGIN": {
        "flags": "T",
        "value": "IGP"
      },
      "ASPATH": {
        "flags": "T",
        "value": [64515, 15169]
      },
      "NEXTHOP": {
        "flags": "T",
        "value": "192.0.2.1"
      },
      "COMMUNITY": {
        "flags": "OT",
        "value": ["64515:100"]
      }
    }
  },
  {}
]
```

## Flowspec Messages

Flowspec (Flow Specification) messages are a special type of UPDATE that carries traffic filtering rules instead of routing prefixes. They use the `MP_REACH` or `MP_UNREACH` attributes with AFI/SAFI set to `IPV4/FLOWSPEC` or `IPV6/FLOWSPEC`.

### Flowspec in MP_REACH

```json
"MP_REACH": {
  "flags": "OX",
  "value": {
    "af": "IPV4/FLOWSPEC",
    "nexthop": "0.0.0.0",
    "rules": [ ... ]
  }
}
```

### Flowspec Rules

Each Flowspec rule is a JSON object with components (match conditions):

```json
{
  "DST": "192.0.2.0/24",
  "SRC": "198.51.100.0/24",
  "PROTO": [{"op": "==", "val": 6}],
  "PORT_DST": [{"op": "==", "val": 80}]
}
```

#### Flowspec Components

| Component | Type | Description |
|-----------|------|-------------|
| `DST` | prefix | Destination prefix |
| `SRC` | prefix | Source prefix |
| `PROTO` | operators | IP protocol (e.g., 6=TCP, 17=UDP) |
| `PORT` | operators | Source or destination port |
| `PORT_DST` | operators | Destination port |
| `PORT_SRC` | operators | Source port |
| `ICMP_TYPE` | operators | ICMP type |
| `ICMP_CODE` | operators | ICMP code |
| `TCP_FLAGS` | bitmask ops | TCP flags (bitmask matching) |
| `PKTLEN` | operators | Packet length |
| `DSCP` | operators | DSCP value |
| `FRAG` | bitmask ops | Fragmentation flags (bitmask matching) |
| `LABEL` | operators | IPv6 flow label (IPv6 only) |

#### Numeric Operators

For components like `PROTO`, `PORT_DST`, etc., the value is an array of operator objects:

```json
[
  {"op": "==", "val": 80},
  {"op": ">=", "val": 1024, "and": true}
]
```

**Operator types**:

- `==`: Equal
- `>`: Greater than
- `>=`: Greater than or equal
- `<`: Less than
- `<=`: Less than or equal
- `!=`: Not equal
- `true`: Always match
- `false`: Never match

**Optional fields**:

- `and: true`: Logical AND with next condition (default is OR)

#### Bitmask Operators

For `TCP_FLAGS` and `FRAG` components:

```json
[
  {"op": "ALL", "val": "0x12", "len": 1}
]
```

**Bitmask operations**:

- `ANY`: Match if any bit is set
- `ALL`: Match if all bits are set
- `NONE`: Match if no bits are set
- `NOT-ALL`: Match if not all bits are set

**Fields**:

- `val`: Hex string (e.g., `0x12`) representing the bitmask
- `len`: Length in bytes (1, 2, 4, or 8)

#### IPv6 Prefix with Offset

For IPv6 Flowspec, prefixes can specify an offset:

```json
"DST": "2001:db8::/32-64"
```

Format: `address/offset-length` where offset is the bit position to start matching from.

### Flowspec Actions (Extended Communities)

Flowspec rules typically have associated actions in extended communities:

```json
"EXT_COMMUNITY": {
  "flags": "OT",
  "value": [
    {"type": "REDIR_IP4", "ip": "192.0.2.1", "val": 100},
    {"type": "RATE", "rate": 0}
  ]
}
```

**Common Flowspec actions**:

- **REDIR_IP4** / **REDIR_IP6**: Redirect traffic to VRF
- **RATE**: Rate limit in bytes/second (0 = discard)
- **MARK**: DSCP marking value
- **RT**: Route Target (for VRF import/export)

### Complete Flowspec Example

This example blocks TCP traffic to port 80 from a specific prefix:

```json
[
  "R",
  15,
  "2025-07-11T10:50:00.000",
  "UPDATE",
  {
    "attrs": {
      "ORIGIN": {
        "flags": "T",
        "value": "IGP"
      },
      "ASPATH": {
        "flags": "T",
        "value": [65055]
      },
      "MP_REACH": {
        "flags": "OX",
        "value": {
          "af": "IPV4/FLOWSPEC",
          "nexthop": "0.0.0.0",
          "rules": [
            {
              "DST": "192.0.2.0/24",
              "PROTO": [{"op": "==", "val": 6}],
              "PORT_DST": [{"op": "==", "val": 80}]
            }
          ]
        }
      },
      "EXT_COMMUNITY": {
        "flags": "OT",
        "value": [
          {"type": "RATE", "rate": 0}
        ]
      }
    }
  },
  {}
]
```

## Implementation Notes

- **Hex Encoding**: When the upper layer is not parsed (e.g., unsupported attribute), the JSON value is a hex string like `0x1234abcd`.
- **Time Format**: Timestamps use `YYYY-MM-DDTHH:MM:SS.mmm` format (millisecond precision).
- **AFI/SAFI Format**: Address family identifiers use `AFI/SAFI` format (e.g., `IPV4/UNICAST`, `IPV6/FLOWSPEC`).
- **Prefixes**: IP prefixes use standard CIDR notation (e.g., `192.0.2.0/24`, `2001:db8::/32`).
- **Bidirectional**: The JSON format fully supports both encoding (JSON to BGP wire) and decoding (BGP wire to JSON).

## See Also

- [Message Filters](filters.md) - Filter BGP messages using the `grep` and `drop` stages
- [Examples](examples.md) - Practical bgpipe command-line examples
- [bgpfix library](https://github.com/bgpfix/bgpfix) - The underlying Go library implementing this format
