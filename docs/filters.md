## Overview

This page explains how to write BGP message filters, used eg. in the `grep` and `drop` stages in `bgpipe`.
Filters let you keep (`grep`) or remove (`drop`) BGP messages based on message type,
NLRI/prefixes, next-hop, AS path, communities, tags, and more.

Summary:

- Use `grep <FILTER>` to keep only the messages that match FILTER
- Use `drop <FILTER>` to remove the messages that match FILTER
- Use `<stage> --if <CONDITION>` to skip a stage completely if the message does not match CONDITION first (`--if` here corresponds to stage **i**nput **f**ilter)
- Combine multiple expressions with `&&` (AND) and `||` (OR)
- Use `(...)` to group and `!` to negate expressions

Examples:

```bash
# keep only IPv6 updates from AS65000
bgpipe -o read updates.mrt.gz \
  -- grep 'ipv6 && as_origin == 65000'

# drop non-IPv6 updates from AS_PATHs that end with ASN matching 204[0-9]+
bgpipe -o read updates.mrt.gz \
  -- drop '!ipv6 && as_path ~ ,204[0-9]+$'

# only for UPDATEs originated by AS15169, drop if no prefixes match 8.0.0.0/8
bgpipe -o read updates.mrt.gz \
  -- grep --if 'as_origin == 15169' 'prefix ~ 8.0.0.0/8'
```

## Filter Syntax

A filter is made of one or more expressions:

* Expression - one of:
    * `attribute` or `attribute[index]`
    * `attribute operator value` or `attribute[index] operator value`
* Chain expressions with `&&` (AND) and `||` (OR)
* Group with `( ... )`
* Negate with `!`

Where:

* `attribute`: which message attribute you want to test, eg. `prefix`, `aspath`, etc.
* `[index]`: an optional selector within that attribute (e.g., `aspath[1]`, `tag[env]`)
* `operator`: value comparison operator (see below)
* `value`: value to compare against (strings can be double-quoted with `"..."`)

Supported operators:

- `==` or `=`: equality
- `!=`: inequality (implemented as negated equality)
- `<`, `<=`, `>`, `>=`: numeric comparisons (where applicable)
- `~`: match (attribute-specific, e.g., prefix overlap, membership)
- `!~`: negative match (negated membership)

Values:

- Unquoted tokens, or quoted strings `"..."` (supports `\\` escaping)
- Numbers are parsed as integers or floats (0x... supported for ints)

Important: Most attributes apply to UPDATE messages only. If your filter
uses UPDATE-only attributes (e.g., prefix/aspath/communities) and the message
is not an UPDATE (e.g., OPEN/KEEPALIVE), that expression evaluates to false.
Use `type` conditions if you want to match non-UPDATE messages.

## Attributes

Below are the attributes you can use. Some keywords are
shortcuts that expand to comparisons on `type` or `af`.

### Message type

- `type`: explicit type comparison
- Shortcuts: `update`, `open`, `keepalive`

Examples:

```text
update                     # same as: type == UPDATE
open || keepalive          # match session control messages
!update || type == OPEN    # only OPEN, not UPDATE
```

### Address family

- `af`: address family (AFI/SAFI)
- Shortcuts: `ipv4` (`af == IPV4/UNICAST`), `ipv6` (`af == IPV6/UNICAST`)

Examples:

```text
ipv4 && update
ipv6 && prefix ~ 2001:db8::/32
```

### NLRI / prefixes

- `reach`: prefixes in MP_REACH or classic IPv4 reachability
- `unreach`: prefixes in MP_UNREACH or classic IPv4 withdrawals
- `prefix`: union of `reach` and `unreach`

Operators:

- `==` exact match, e.g. `prefix == 203.0.113.0/24`
- `~` overlap (subnet) test, e.g. `prefix ~ 203.0.113.0/24`

Examples:

```text
prefix ~ 8.0.0.0/8            # any prefix overlapping 8/8
reach == 203.0.113.0/24       # exact announcement of 203.0.113.0/24
unreach ~ 2001:db8::/32       # any withdrawal inside 2001:db8::/32
```

### Next-hop

- `nexthop` or `nh`: match next-hop IP

Examples:

```text
nh == 192.0.2.1
nexthop ~ 2001:db8::/64
```

### AS path

 * `aspath` matches full AS_PATH
 * `aspath[index]` matches particular index
 * Shortcuts:
    * `as_origin` is origin AS (rightmost, index -1)
    * `as_upstream` is upstream of origin (index -2)
    * `as_peer` is neighbor/peer AS (leftmost, index 0)

Indexing rules:

- Numeric indexes pick a specific position: `aspath[0]` is the leftmost AS (peer).
- Negative indexes count from the right: `aspath[-1]` is the origin; `aspath[-2]` upstream of origin.
- If you omit the index on the shortcuts, they assume the position above (e.g., `as_origin` implies `[-1]`).

Examples:

```text
as_origin == 64496
as_peer != 64512
aspath[1] == 3356
aspath[-2] == 3356
```

### Communities

- `community` or `com`: standard communities
- `com_ext`, `ext_community`, `ext_com`: extended communities
- `com_large`, `large_community`, `large_com`: large communities

Operators and values:

- Use `==` for exact string match
- Use `~` to match against a regular expression
- Community formats are strings like `"65000:1"` or `"1234:5678:90"`

Examples:

```text
community ~ "3356:"
com_large ~ "1234:5678:9"
ext_community ~ "rt:65000:1"
```

### Tags (pipeline context)

Tags are key/value pairs attached to messages by the `tag` stage.
You can match them with the `tag[...]` attribute.

Indexing rules for tags:

 * the index is a string key, e.g., `tag[env]`, `tags[region]`
 * `==` compares the exact tag value
 * `~` can be used for pattern-like matches

Examples:

```text
tag[env] == prod
tags[region] ~ "^eu-"
```
