# Flowspec JSON Format

Flowspec (Flow Specification, [RFC 8955](https://datatracker.ietf.org/doc/html/rfc8955)) carries traffic filtering policies over BGP.
A Flowspec UPDATE message consists of two parts:

- **Rules** — traffic match conditions, carried in `MP_REACH` (or `MP_UNREACH` for withdrawals)
- **Actions** — what to do with matched traffic, carried in `EXT_COMMUNITY`

## Complete Example

Block all TCP port 80 traffic to 192.0.2.0/24:

```json
[
  "R", 15, "2025-07-11T10:50:00.000", "UPDATE",
  {
    "attrs": {
      "ORIGIN": {"flags": "T", "value": "IGP"},
      "ASPATH": {"flags": "T", "value": [65055]},
      "MP_REACH": {
        "flags": "OX",
        "value": {
          "af": "IPV4/FLOWSPEC",
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
          {"type": "FLOW_RATE_BYTES", "value": 0}
        ]
      }
    }
  },
  null
]
```

The **rule** in `MP_REACH` matches TCP (protocol 6) traffic to port 80 destined for 192.0.2.0/24.
The **action** in `EXT_COMMUNITY` rate-limits matching traffic to 0 bytes/sec (i.e., drops it).

## Flowspec Rules

Rules are carried in the `MP_REACH` attribute with a Flowspec address family (`IPV4/FLOWSPEC` or `IPV6/FLOWSPEC`). Each rule is a JSON object mapping component names to match conditions:

```json
"MP_REACH": {
  "flags": "OX",
  "value": {
    "af": "IPV4/FLOWSPEC",
    "rules": [
      {
        "DST": "192.0.2.0/24",
        "SRC": "198.51.100.0/24",
        "PROTO": [{"op": "==", "val": 6}],
        "PORT_DST": [{"op": "==", "val": 80}]
      }
    ]
  }
}
```

Withdrawals use `MP_UNREACH` with the same structure (without `nexthop`).

### Components

| Component    | Type       | Description                                        |
|--------------|------------|----------------------------------------------------|
| `DST`        | prefix     | Destination IP prefix                              |
| `SRC`        | prefix     | Source IP prefix                                   |
| `PROTO`      | numeric    | IP protocol number (6 = TCP, 17 = UDP)             |
| `PORT`       | numeric    | Source or destination port                          |
| `PORT_DST`   | numeric    | Destination port                                   |
| `PORT_SRC`   | numeric    | Source port                                        |
| `ICMP_TYPE`  | numeric    | ICMP type                                          |
| `ICMP_CODE`  | numeric    | ICMP code                                          |
| `TCP_FLAGS`  | bitmask    | TCP flags                                          |
| `PKTLEN`     | numeric    | IP packet length                                   |
| `DSCP`       | numeric    | DSCP value                                         |
| `FRAG`       | bitmask    | IP fragmentation flags                             |
| `LABEL`      | numeric    | IPv6 flow label (IPv6 Flowspec only)               |

The **Type** column indicates which operator format the component uses: see [Numeric Operators](#numeric-operators) or [Bitmask Operators](#bitmask-operators) below.

### IPv6 Prefix with Offset

IPv6 Flowspec prefixes can include a bit offset:

```json
"DST": "2001:db8::/0-32"
```

Format: `address/offset-length` where offset is the bit position to start matching from.

## Numeric Operators

Components of the **numeric** type (`PROTO`, `PORT`, `PORT_DST`, `PORT_SRC`, `ICMP_TYPE`, `ICMP_CODE`, `PKTLEN`, `DSCP`, `LABEL`) use arrays of operator objects:

```json
"PORT_DST": [
  {"and": true, "op": ">=", "val": 8000},
  {"op": "<=", "val": 9000}
]
```

This matches destination ports 8000-9000: the first condition matches >= 8000 and is ANDed with the next condition (<= 9000).

| Field | Type    | Description                                                   |
|-------|---------|---------------------------------------------------------------|
| `op`  | string or boolean | Comparison operator (see [Numeric Operator Values](#numeric-operator-values)) |
| `val` | integer | Value to compare against                                      |
| `and` | boolean | If `true`, AND the result of this condition with the next one. Default: OR with the next condition. |

Multiple conditions in the array are combined with **OR** by default. Set `"and": true` on a condition to **AND** it with the following condition instead.

### Numeric Operator Values

| Operator | Meaning              |
|----------|----------------------|
| `==`     | Equal                |
| `!=`     | Not equal            |
| `>`      | Greater than         |
| `>=`     | Greater than or equal |
| `<`      | Less than            |
| `<=`     | Less than or equal   |
| `true`   | Always match         |
| `false`  | Never match          |

## Bitmask Operators

Components of the **bitmask** type (`TCP_FLAGS`, `FRAG`) use arrays of bitmask operator objects:

```json
"TCP_FLAGS": [
  {"op": "ALL", "val": "0x02"}
]
```

This matches packets with the SYN flag set.

| Field | Type    | Description                                                   |
|-------|---------|---------------------------------------------------------------|
| `op`  | string  | Bitmask operation (see [Bitmask Operation Values](#bitmask-operation-values)) |
| `val` | string  | Hex bitmask (e.g., `"0x02"`)                                  |
| `len` | integer | Value length in bytes (1, 2, 4, 8)                            |
| `and` | boolean | If `true`, AND the result of this condition with the next one. Default: OR with the next condition. |

### Bitmask Operation Values

| Operation | Meaning                       |
|-----------|-------------------------------|
| `ANY`     | Match if any specified bit set |
| `ALL`     | Match if all specified bits set |
| `NONE`    | Match if no specified bits set |
| `NOT-ALL` | Match if not all bits set     |

## Flowspec Actions

Actions define what happens to traffic matching the Flowspec rules. They are encoded as extended communities in the `EXT_COMMUNITY` attribute of the same UPDATE message.

| Action                | Extended community type | Value                           | Description                    |
|-----------------------|------------------------|---------------------------------|--------------------------------|
| Drop traffic          | `FLOW_RATE_BYTES`      | `0`                             | Rate-limit to 0 bytes/sec      |
| Rate-limit            | `FLOW_RATE_BYTES`      | `1500.5` or `"id:rate"`         | Limit bytes/sec                |
| Rate-limit (packets)  | `FLOW_RATE_PACKETS`    | `100` or `"id:rate"`            | Limit packets/sec              |
| Traffic action        | `FLOW_ACTION`          | `{"terminal": bool, "sample": bool}` | Terminal/sample flags   |
| Redirect (2-byte ASN) | `FLOW_REDIRECT_AS2`    | `"ASN:value"`                   | Redirect to VRF                |
| Redirect (IPv4)       | `FLOW_REDIRECT_IP4`    | `"IP:value"`                    | Redirect to VRF (IP-based)     |
| Redirect (4-byte ASN) | `FLOW_REDIRECT_AS4`    | `"ASN:value"`                   | Redirect to VRF (4-byte ASN)   |
| Redirect to next-hop  | `FLOW_REDIRECT_NH`     | `{"copy": bool}`                | Redirect via next-hop          |
| DSCP marking          | `FLOW_DSCP`            | integer                         | Set DSCP value                 |

### Example: Rate-limit with Redirect

Rate-limit UDP traffic from 198.51.100.0/24 to 10 Mbps and redirect to VRF:

```json
"EXT_COMMUNITY": {
  "flags": "OT",
  "value": [
    {"type": "FLOW_RATE_BYTES", "value": 1250000},
    {"type": "FLOW_REDIRECT_AS2", "value": "65055:100"}
  ]
}
```

## See Also

- [JSON Format](json-format.md) — General BGP message JSON format
- [Message Filters](filters.md) — Filter BGP messages
- [bgpfix library](https://github.com/bgpfix/bgpfix) — The underlying Go library
