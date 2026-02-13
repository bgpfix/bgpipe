# Message Filters

Filters let you select BGP messages based on type, prefixes, AS path, origin, MED, LOCAL_PREF, OTC, communities, tags, timestamps, and arbitrary JSON paths.
They are used by the [`grep`](stages/grep.md) and [`drop`](stages/grep.md) stages, and by the `--if` and `--of` options on any stage.

- `grep FILTER` — keep only matching messages
- `drop FILTER` — remove matching messages
- `STAGE --if FILTER` — skip stage processing for non-matching messages (input filter)
- `STAGE --of FILTER` — drop non-matching stage output (output filter)

## Quick Examples

```bash
# keep only IPv6 updates from AS65000
bgpipe -o read updates.mrt.gz -- grep 'ipv6 && as_origin == 65000'

# drop non-IPv6 updates where AS_PATH ends with ASN matching 204xxx
bgpipe -o read updates.mrt.gz -- drop '!ipv6 && aspath ~ ,204[0-9]+$'

# only for UPDATEs from AS15169, keep those with prefixes overlapping 8.0.0.0/8
bgpipe -o read updates.mrt.gz -- grep --if 'as_origin == 15169' 'prefix ~ 8.0.0.0/8'

# drop routes with long AS paths or incomplete origin
bgpipe -o read updates.mrt.gz -- drop 'aspath_len > 10 || origin == incomplete'

# keep only routes with local_pref above default
bgpipe -o read updates.mrt.gz -- grep 'local_pref > 100'
```

## Syntax

A filter is one or more expressions chained with `&&` (AND) and `||` (OR):

```
[!] attribute[index] [operator value] [&& | ||] ...
```

- `( ... )` — group expressions
- `!` — negate an expression
- `attribute` — what to test (e.g., `prefix`, `aspath`, `community`)
- `[index]` — optional selector within the attribute
- `operator value` — comparison; when omitted, tests for attribute existence

### Operators

| Operator  | Aliases  | Meaning                                  |
|-----------|----------|------------------------------------------|
| `==`      | `=`      | Equal                                    |
| `!=`      | `=!`     | Not equal                                |
| `<`       |          | Less than (attribute-specific semantics) |
| `<=`      |          | Less than or equal                       |
| `>`       |          | Greater than (attribute-specific semantics) |
| `>=`      |          | Greater than or equal                    |
| `~`       |          | Match (attribute-specific: overlap, regex, containment) |
| `!~`      | `~!`     | Negative match                           |

### Values

- Unquoted tokens: `65000`, `8.0.0.0/8`, `UPDATE`
- Quoted strings: `"65000:100"` (supports `\\` escaping)
- Numbers: integers, floats, hex (`0x...`)

### Important Notes

- Most attributes apply to **UPDATE messages only**. Non-UPDATE messages (OPEN, KEEPALIVE, etc.) evaluate to `false` for UPDATE-only attributes. Use `type` conditions for non-UPDATE matching.
- The `~` operator uses **Go regexp syntax** (not shell globs) when matching strings.
- `&&` and `||` are evaluated **left to right** with short-circuit. Use parentheses for explicit grouping when mixing operators: `(A && B) || C` instead of `A && B || C`.

## Attributes

### Message Type

| Attribute    | Operators | Description                              |
|-------------|-----------|------------------------------------------|
| `type`      | `==`, `!=` | Explicit type comparison: `UPDATE`, `OPEN`, `KEEPALIVE`, `NOTIFY`, `REFRESH` |

**Shortcuts** (no operator needed):

| Shortcut    | Equivalent           |
|-------------|----------------------|
| `update`    | `type == UPDATE`     |
| `open`      | `type == OPEN`       |
| `keepalive` | `type == KEEPALIVE`  |

Examples:

```text
update                     # match UPDATE messages
open || keepalive          # match session control messages
!update                    # match everything except UPDATEs
type == NOTIFY             # match NOTIFICATION messages
```

### Address Family

| Attribute | Operators | Description                              |
|-----------|-----------|------------------------------------------|
| `af`      | `==`, `!=` | Address family (AFI/SAFI). |

**Shortcuts:**

| Shortcut | Equivalent           |
|----------|----------------------|
| `ipv4`   | `af == IPV4/UNICAST` |
| `ipv6`   | `af == IPV6/UNICAST` |

The `af` value can match by full AFI/SAFI (e.g., `IPV4/UNICAST`), by AFI alone (e.g., `IPV4`), or by SAFI alone (e.g., `UNICAST`).

Examples:

```text
ipv4 && update                   # IPv4 unicast updates
ipv6                             # IPv6 unicast
af == IPV4/FLOWSPEC              # IPv4 Flowspec
af == IPV6                       # any IPv6 (unicast, multicast, etc.)
af != UNICAST                    # non-unicast SAFI (e.g., flowspec, multicast)
```

### Prefixes (NLRI)

| Attribute | Operators                    | Description                            |
|-----------|------------------------------|----------------------------------------|
| `prefix`  | `==`, `~`, `<`, `<=`, `>`, `>=` | Any prefix (reach or unreach)      |
| `reach`   | `==`, `~`, `<`, `<=`, `>`, `>=` | Announced prefixes only            |
| `unreach` | `==`, `~`, `<`, `<=`, `>`, `>=` | Withdrawn prefixes only            |

Prefixes include both classic IPv4 NLRI and MP-BGP (MP_REACH/MP_UNREACH) prefixes.

**Operator semantics for prefixes:**

| Operator | Meaning                                                                 |
|----------|-------------------------------------------------------------------------|
| `==`     | Exact match: same address and prefix length                             |
| `~`      | Overlap: message prefix and reference prefix overlap in any way         |
| `<`      | Message prefix is **more specific** (longer) than reference, and overlaps |
| `<=`     | Message prefix is more specific or equal, and overlaps                  |
| `>`      | Message prefix is **less specific** (shorter) than reference, and overlaps |
| `>=`     | Message prefix is less specific or equal, and overlaps                  |

By default, a match succeeds if **any** prefix in the message matches. Use `prefix[*]` to require **all** prefixes to match.

Examples:

```text
prefix ~ 8.0.0.0/8             # any prefix overlapping 8.0.0.0/8
reach == 203.0.113.0/24        # exact announcement
unreach ~ 2001:db8::/32        # any IPv6 withdrawal overlapping 2001:db8::/32
prefix < 10.0.0.0/8            # more specific than /8 (e.g., 10.1.0.0/16)
prefix > 10.1.0.0/16           # less specific than /16 (e.g., 10.0.0.0/8)
prefix[*] ~ 8.0.0.0/8          # ALL prefixes in message overlap 8.0.0.0/8
```

### Next-Hop

| Attribute    | Operators                    | Description              |
|-------------|------------------------------|--------------------------|
| `nexthop`   | `==`, `~`, `<`, `<=`, `>`, `>=` | Next-hop IP address  |
| `nh`        | *(alias for `nexthop`)*      |                          |

**Operator semantics for next-hop:**

| Operator | Meaning                                               |
|----------|-------------------------------------------------------|
| `==`     | Exact IP address match                                |
| `~`      | Next-hop is **contained** in the given CIDR prefix    |
| `<`, `<=`, `>`, `>=` | Numeric IP address comparison            |

Examples:

```text
nh == 192.0.2.1                # exact next-hop match
nexthop ~ 2001:db8::/64        # next-hop within this IPv6 prefix
nexthop ~ 0.0.0.0/0            # any next-hop (always matches)
```

### AS Path

| Attribute      | Operators                    | Description                     |
|---------------|------------------------------|---------------------------------|
| `aspath`      | `==`, `~`, `<`, `<=`, `>`, `>=` | Any hop (int) or full path (string/regex) |
| `aspath[N]`   | `==`, `<`, `<=`, `>`, `>=`  | Specific hop by index           |
| `aspath[*]`   | `==`, `<`, `<=`, `>`, `>=`  | Any hop (explicit, including AS_SET members) |
| `as_origin`   | `==`, `<`, `<=`, `>`, `>=`  | Origin AS (last hop, index -1)  |
| `as_upstream`  | `==`, `<`, `<=`, `>`, `>=` | Upstream of origin (index -2)   |
| `as_peer`     | `==`, `<`, `<=`, `>`, `>=`  | Peer AS (first hop, index 0)    |
| `aspath_len`  | `==`, `<`, `<=`, `>`, `>=`  | AS_PATH length (hop count)      |
| `aspath_hops` | `==`, `<`, `<=`, `>`, `>=`  | AS_PATH unique hops (ignoring prepending) |

**Index rules:**

- Positive: `aspath[0]` is the first (leftmost) AS, `aspath[1]` is second, etc.
- Negative: `aspath[-1]` is the last (origin) AS, `aspath[-2]` is the upstream, etc.
- Wildcard: `aspath[*]` matches any hop (equivalent to no index for int comparisons).

**Operator semantics:**

| Operator | Without index                          | With index                       |
|----------|----------------------------------------|----------------------------------|
| (none)   | AS_PATH exists and is non-empty        | —                                |
| `==` (int) | **Any hop** equals the value         | Specific hop equals the value    |
| `==` (string) | Full path string matches exactly | —                                |
| `~` (regex) | Regex match on JSON path text       | —                                |
| `<`, `<=`, `>`, `>=` | **Any hop** ASN satisfies comparison | Specific hop ASN satisfies comparison |

The `~` regex matches against the JSON representation of the AS path (comma-separated ASNs without brackets).

Examples:

```text
as_origin == 15169             # originated by AS15169
as_peer != 64512               # peer is not AS64512
aspath[1] == 3356              # second hop is AS3356
aspath[-2] == 3356             # upstream of origin is AS3356
aspath ~ ",15169,"             # AS15169 appears anywhere in the path
aspath ~ "^15169,"             # path starts with AS15169
as_origin > 64511              # origin ASN is in the private range (> 64511)
aspath_len > 5                 # reject long paths
aspath_len == 0                # no AS_PATH (direct peering or incomplete)
aspath_hops == 2               # exactly 2 unique ASNs (e.g., 65001 65001 3356 has len=3 but hops=2)
aspath_hops > 5                # more than 5 unique hops (ignoring prepending)
```

### Origin

| Attribute | Operators | Description                              |
|-----------|-----------|------------------------------------------|
| `origin`  | `==`, `!=` | BGP ORIGIN attribute                   |

Values: `igp` (or `i`, `0`), `egp` (or `e`, `1`), `incomplete` (or `?`, `2`).

Without an operator, tests for the attribute's existence.

Examples:

```text
origin == igp                  # originated via IGP
origin != incomplete           # not incomplete origin
origin                         # has ORIGIN attribute set
```

### MED

| Attribute       | Operators                    | Description              |
|-----------------|------------------------------|--------------------------|
| `med`           | `==`, `<`, `<=`, `>`, `>=`   | Multi-Exit Discriminator |
| `metric`        | *(alias for `med`)*          |                          |

Without an operator, tests for the attribute's existence.

Examples:

```text
med == 0                       # MED is zero
med > 100                      # MED above 100
med                            # has MED attribute
```

### LOCAL_PREF

| Attribute       | Operators                    | Description              |
|-----------------|------------------------------|--------------------------|
| `local_pref`    | `==`, `<`, `<=`, `>`, `>=`   | LOCAL_PREF value         |
| `localpref`     | *(alias for `local_pref`)*   |                          |

Without an operator, tests for the attribute's existence.

Examples:

```text
local_pref == 100              # default local preference
local_pref > 100               # preferred routes
localpref <= 50                # low-preference routes
```

### OTC (Only To Customer)

| Attribute            | Operators                    | Description                         |
|----------------------|------------------------------|-------------------------------------|
| `otc`                | `==`, `<`, `<=`, `>`, `>=`   | OTC attribute value (ASN, [RFC 9234](https://www.rfc-editor.org/rfc/rfc9234)) |
| `only_to_customer`   | *(alias for `otc`)*          |                                     |

Without an operator, tests for the attribute's existence.

Examples:

```text
otc                            # has OTC attribute
otc == 65001                   # OTC value is AS65001
otc > 64511                    # OTC ASN in the private range
!otc                           # no OTC attribute (route leak candidate)
```

### Communities

| Attribute                         | Operators     | Description                |
|-----------------------------------|---------------|----------------------------|
| `community`, `com`                | `==`, `~`     | Standard communities       |
| `ext_community`, `ext_com`, `com_ext` | `==`, `~` | Extended communities       |
| `large_community`, `large_com`, `com_large` | `==`, `~` | Large communities    |

**Operator semantics:**

| Operator | Meaning                                                           |
|----------|-------------------------------------------------------------------|
| (none)   | Community attribute exists (has any value)                        |
| `==`     | Message has an exact community value                              |
| `~`      | Regex match against **JSON text** of all communities              |

The `~` regex matches against the JSON representation. For standard communities, this is `"ASN:VALUE"` strings. For extended communities, the JSON contains the full structure with type names like `TARGET`, `IP4_TARGET`, etc. (see [JSON Format](json-format.md#communities)).

Examples:

```text
community                      # has any standard community
community == "3356:100"        # has exact community 3356:100
community ~ "3356:"            # any community with ASN 3356
community !~ "3356:"           # no standard community from ASN 3356
com_large ~ "1234:5678:9"      # large community matching pattern
ext_community ~ "TARGET"       # has any Route Target extended community
```

### Tags

| Attribute       | Operators     | Description                          |
|-----------------|---------------|--------------------------------------|
| `tag`, `tags`   | `==`, `~`     | Pipeline metadata tags               |
| `tag[KEY]`      | `==`, `~`     | Specific tag by key name             |

Tags are key-value pairs attached to messages by the [`tag`](stages/tag.md) stage and other stages (e.g., `ris-live` adds `PEER_AS`, `PEER_IP`; `rpki` adds `rpki/status`).

Tag filters work on **all message types** (not just UPDATEs).

| Operator | Without index                         | With `[KEY]`                        |
|----------|---------------------------------------|-------------------------------------|
| (none)   | Any tag has a non-empty value         | Tag KEY has a non-empty value       |
| `==`     | Any tag value equals the string       | Tag KEY equals the string           |
| `~`      | Any tag value matches the regex       | Tag KEY matches the regex           |

Examples:

```text
tag[rpki/status] == INVALID    # RPKI validation failed
tag[PEER_AS] == "8218"         # from RIS peer AS8218
tags[region] ~ "^eu-"          # region tag starts with "eu-"
tag[rpki/status] != VALID      # anything except VALID
```

### Direction

| Attribute       | Operators | Description              |
|-----------------|-----------|--------------------------|
| `dir`           | `==`, `!=` | Message direction       |
| `direction`     | *(alias for `dir`)*      |          |

Values: `L` (left/local) or `R` (right/remote). Case-insensitive.

Without an operator, tests whether the direction is set (non-zero). Works on **all message types**.

Examples:

```text
dir == L                           # left-direction messages
dir == R                           # right-direction messages
dir != L                           # not left-direction
```

### Sequence Number

| Attribute       | Operators                    | Description              |
|-----------------|------------------------------|--------------------------|
| `seq`           | `==`, `<`, `<=`, `>`, `>=`   | Message sequence number  |
| `sequence`      | *(alias for `seq`)*          |                          |

Without an operator, tests whether the sequence number is non-zero. Works on **all message types**.

Examples:

```text
seq > 0                            # has a sequence number
seq == 42                          # exact sequence number
seq >= 100 && seq < 200            # sequence number range
```

### Timestamp

| Attribute       | Operators                         | Description              |
|-----------------|-----------------------------------|--------------------------|
| `time`          | `==`, `~`, `<`, `<=`, `>`, `>=`   | Message timestamp        |
| `timestamp`     | *(alias for `time`)*              |                          |

The timestamp is formatted as ISO 8601: `2006-01-02T15:04:05.000`. Comparison operators use **lexicographic string comparison**, which is correct for this fixed-width format.

Without an operator, tests whether the timestamp is non-zero. Works on **all message types**.

Examples:

```text
time                               # timestamp is set (non-zero)
time ~ "^2023-03"                  # March 2023
time > "2023-01-01"                # after start of 2023
time == "2023-03-01T12:30:45.000"  # exact timestamp
time >= "2023-01-01" && time < "2024-01-01"  # all of 2023
```

### JSON Path

| Attribute       | Operators                         | Description                          |
|-----------------|-----------------------------------|--------------------------------------|
| `json[PATH]`    | `==`, `~`, `<`, `<=`, `>`, `>=`   | Extract from message upper layer JSON |

Extracts a value from the message's upper layer JSON (the UPDATE/OPEN object) using a dot-separated path. The path segments are passed to a JSON path extractor.

Without an operator, tests for the path's existence. Numeric operators (`<`, `<=`, `>`, `>=`) parse the extracted value as a number — if it's not a number, the expression evaluates to false. The `==` operator compares extracted text as a string. The `~` operator runs a regex against the raw JSON text at the path.

Works on **all message types** (returns false for KEEPALIVE which has no upper layer).

Examples:

```text
json[reach]                                # has reachable prefixes
json[attrs.ORIGIN.value] == IGP            # ORIGIN is IGP
json[attrs.MED.value] > 100               # MED above 100
json[attrs.COMMUNITY.value] ~ "3356:"     # community text contains "3356:"
json[attrs.MP_REACH.value.af]              # MP_REACH has af field
```

### Attribute Path

| Attribute       | Operators                         | Description                          |
|-----------------|-----------------------------------|--------------------------------------|
| `attr[NAME]`     | `==`, `~`, `<`, `<=`, `>`, `>=`  | Shortcut for `json[attrs.NAME.value]` |
| `attr[NAME.sub]` | `==`, `~`, `<`, `<=`, `>`, `>=`  | Navigate into attribute value        |

Convenience shortcut for accessing BGP attribute values via JSON. `attr[ORIGIN]` is equivalent to `json[attrs.ORIGIN.value]`, and `attr[MP_REACH.af]` is equivalent to `json[attrs.MP_REACH.value.af]`.

The attribute name is resolved against known BGP attribute codes (case-insensitive). Unknown attribute names produce a parse error.

Without an operator, tests for the attribute's existence. Operator semantics are the same as `json[PATH]`.

Examples:

```text
attr[ORIGIN] == IGP                # ORIGIN is IGP
attr[MED] > 100                    # MED above 100
attr[COMMUNITY] ~ "3356:"         # any community from AS3356
attr[MP_REACH.af]                  # MP_REACH has af field
attr[LOCALPREF] >= 200             # high local preference
```

## Operator Compatibility

Not all operators work with all attributes. This table summarizes:
`!=` and `!~` are supported wherever `==` and `~` are supported (they are parsed as negated forms).

| Attribute       | (exists) | `==` | `<` `<=` `>` `>=` | `~` `!~`  |
|-----------------|----------|------|--------------------|-----------|
| `type`          |          | yes  |                    |           |
| `af`            |          | yes  |                    |           |
| `prefix`        | yes      | yes  | yes (specificity)  | yes (overlap) |
| `nexthop`       | yes      | yes  | yes (numeric IP)   | yes (containment) |
| `aspath`        | yes      | yes  | yes (ASN value)    | yes (regex) |
| `aspath_len`    | yes      | yes  | yes (hop count)    |           |
| `aspath_hops`   | yes      | yes  | yes (unique hops)  |           |
| `origin`        | yes      | yes  |                    |           |
| `med`           | yes      | yes  | yes (uint32)       |           |
| `local_pref`    | yes      | yes  | yes (uint32)       |           |
| `otc`           | yes      | yes  | yes (uint32)       |           |
| `community`     | yes      | yes  |                    | yes (regex) |
| `ext_community` | yes      | yes  |                    | yes (regex) |
| `large_community`| yes     | yes  |                    | yes (regex) |
| `tag`           | yes      | yes  |                    | yes (regex) |
| `dir`           | yes      | yes  |                    |           |
| `seq`           | yes      | yes  | yes (int64)        |           |
| `time`          | yes      | yes  | yes (string)       | yes (regex) |
| `json[PATH]`    | yes      | yes  | yes (numeric)      | yes (regex) |
| `attr[NAME]`    | yes      | yes  | yes (numeric)      | yes (regex) |

## See Also

- [grep / drop](stages/grep.md) — Stages that use filters
- [JSON Format](json-format.md) — BGP message JSON structure (community `~` matches against this)
- [Examples](examples.md) — Practical bgpipe pipelines
