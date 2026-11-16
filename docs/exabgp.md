# ExaBGP

[ExaBGP](https://github.com/Exa-Networks/exabgp) is controlled with text
commands such as `announce route 10.0.0.0/24 next-hop 192.0.2.1`, printed by
helper scripts ("processes") that it runs. bgpipe reads and writes the same
commands, one per line, in every stage that takes `--format exa`:
[stdin](stages/stdin.md), [stdout](stages/stdout.md), [read](stages/read.md),
[write](stages/write.md), [exec](stages/exec.md), [pipe](stages/pipe.md), and
[websocket](stages/websocket.md). Files named `*.exa` are detected
automatically.

This lets you:

- run a script written for ExaBGP under bgpipe, with no ExaBGP involved
- turn MRT files, live feeds, or a BGP session into commands for an ExaBGP
  instance
- keep routes in a plain text file that both tools understand

## Supported commands

```
announce route PREFIX [next-hop ADDR|self] [ATTRIBUTE ...]
withdraw route PREFIX
announce attributes [next-hop ADDR|self] [ATTRIBUTE ...] nlri PREFIX [PREFIX ...]
withdraw attributes nlri PREFIX [PREFIX ...]
announce flow route MATCH ... ACTION ...
withdraw flow route MATCH ...
```

`ipv4 unicast` and `ipv6 unicast` are accepted in place of `route`. Empty lines
and lines starting with `#` are skipped. A leading neighbor selector
(`neighbor 192.0.2.1 announce route ...`) is accepted and ignored: a bgpipe
pipeline is one BGP session, so run one bgpipe per neighbor.

The prefixes after `nlri` go into one UPDATE, so they must be all IPv4 or all
IPv6. See [Flowspec](#flowspec) for `flow route`.

| Attribute | Example | Notes |
|-----------|---------|-------|
| `next-hop` | `next-hop 192.0.2.1` | `self` is our address on the BGP session. An IPv4 next-hop for an IPv6 prefix is sent IPv4-mapped (`::ffff:192.0.2.1`), as ExaBGP does |
| `origin` | `origin igp` | `igp`, `egp`, or `incomplete`; `igp` if not given |
| `as-path` | `as-path [ 65001 65002 ]` | If not given: our ASN on an eBGP session, else empty |
| `med` | `med 100` | |
| `local-preference` | `local-preference 200` | |
| `community` | `community [ 65000:1 no-export ]` | Also `no-advertise`, `no-export-subconfed`, `no-peer`, `blackhole` |
| `large-community` | `large-community [ 65000:1:2 ]` | |
| `extended-community` | `extended-community [ target:65000:1 ]` | `target:` and `origin:` with an ASN or an IPv4 address, or any value as `0x` and 16 hex digits |
| `atomic-aggregate` | `atomic-aggregate` | |
| `aggregator` | `aggregator ( 65000:192.0.2.1 )` | |
| `originator-id` | `originator-id 192.0.2.1` | |
| `cluster-list` | `cluster-list [ 192.0.2.1 ]` | |

Lists follow the ExaBGP syntax: brackets need no spaces around them
(`[65001 65002]`), and a single value needs no brackets (`community 65000:1`).

## What bgpipe takes from the session

As ExaBGP does, bgpipe completes each announcement with what it knows about
the BGP session the commands are sent to:

- `next-hop self` becomes the local address of the connection
- a route with no `as-path` gets our ASN, if the peer is in another AS (eBGP)
- [exec](stages/exec.md) `--read` answers each command with a `done` or `error`
  line on the script's stdin, which ExaBGP scripts may wait for

The first two need the session to be up: start the stage that reads the
commands with `--wait ESTABLISHED`. With no session (eg. when converting
files), the commands are taken as written, and `next-hop self` leaves the
next-hop unset.

## Differences from ExaBGP

- **Other attributes are errors** (eg. `label`, `path-information`, `aigp`,
  `attribute`), as are other commands (`announce eor`, `announce vpls`, ...):
  bgpipe does not announce a route without what was asked for. Add `--pardon`
  to skip such lines instead. Use the [JSON format](json-format.md) when you
  need every attribute.
- **Attributes after `withdraw route PREFIX`** are accepted but not needed.
- **AS sets are flattened** into the AS sequence.
- **Output is one command per UPDATE**: `route` for a single prefix, else
  `attributes ... nlri`, and a `flow route` per Flowspec rule. Other message
  types (OPEN, KEEPALIVE, End-of-RIB) have no command and are skipped.
- **Scripts get commands, not ExaBGP's JSON.** Without `--read`,
  [exec](stages/exec.md) writes the messages of the pipeline to the script as
  commands; scripts that parse ExaBGP's JSON encoding of received routes need
  porting.

## Run an ExaBGP script

An ExaBGP process script prints commands on its stdout. Run it with
[exec](stages/exec.md) and bgpipe announces the routes itself:

```bash
bgpipe \
    -- speaker --active --asn 65001 --id 192.0.2.2 \
    -- exec --format exa --read --wait ESTABLISHED /usr/local/bin/healthcheck.sh \
    -- connect 192.0.2.1
```

- `--wait ESTABLISHED` starts the script once the session is up
- `--read` makes the stage read-only: bgpipe writes nothing but `done` and
  `error` to the script's stdin

For example, this script announces a service prefix and withdraws it when the
service stops responding:

```bash
#!/bin/sh
up=0
while sleep 5; do
    if curl -fs -m 2 -o /dev/null http://127.0.0.1/health; then
        [ $up = 1 ] || echo "announce route 203.0.113.0/24 next-hop self community [65001:100]"
        up=1
    else
        [ $up = 0 ] || echo "withdraw route 203.0.113.0/24"
        up=0
    fi
done
```

ExaBGP's own health checker runs unmodified, too:

```bash
bgpipe \
    -- speaker --active --asn 65001 --id 192.0.2.2 \
    -- exec --format exa --read --wait ESTABLISHED -A \
        exabgp healthcheck --no-syslog --no-ip-setup --ip 203.0.113.1/32 \
        --cmd "curl -fs -m 2 -o /dev/null http://127.0.0.1/health" --withdraw-on-down -- \
    -- connect 192.0.2.1
```

## Flowspec

Flow routes ([RFC 8955](https://datatracker.ietf.org/doc/html/rfc8955)) are
read in both ExaBGP forms, and written in the second, flat one:

```
announce flow route { match { destination 192.0.2.1/32; protocol tcp; destination-port =80; } then { discard; } }
announce flow route destination 192.0.2.1/32 protocol =6 destination-port =80 discard
```

| Match | Example | Notes |
|-------|---------|-------|
| `destination`, `source` | `destination 192.0.2.0/24` | An IPv6 prefix makes it an IPv6 rule; `2001:db8::/64/32` has an offset of 32 bits |
| `protocol`, `next-header` | `protocol [ tcp udp ]` | `next-header` for IPv6. A number, or `icmp`, `igmp`, `tcp`, `egp`, `udp`, `rsvp`, `gre`, `esp`, `ah`, `ospf`, `pim`, `sctp` |
| `port`, `destination-port`, `source-port` | `destination-port [ =80 =443 ]`, `port >1024&<2048` | Numbers only |
| `packet-length` | `packet-length >=1000` | |
| `icmp-type`, `icmp-code` | `icmp-type echo-request` | A number, or `echo-reply`, `unreachable`, `redirect`, `echo-request`, `time-exceeded` |
| `tcp-flags` | `tcp-flags [ syn !ack ]`, `tcp-flags =syn+ack` | `fin`, `syn`, `rst`, `push`, `ack`, `urg`, `ece`, `cwr`, or a number as `0x12` |
| `fragment` | `fragment is-fragment` | `dont-fragment`, `is-fragment`, `first-fragment`, `last-fragment`, or a number |
| `dscp`, `traffic-class` | `dscp =10` | `traffic-class` for IPv6 |
| `flow-label` | `flow-label =5` | IPv6 only |

Numeric values take `=` (the default), `>`, `>=`, `<`, `<=`, `!=`, and `&` to
require both conditions; `tcp-flags` and `fragment` take no operator (any of
the bits), `=` (exactly these bits), `!` (none of them), and `!=`. bgpipe
writes all values as numbers.

| Action | Example | Notes |
|--------|---------|-------|
| `discard` | `discard` | |
| `rate-limit` | `rate-limit 9600`, `rate-limit 1000 packets` | Bytes per second, unless `packets` |
| `redirect` | `redirect 65000:100`, `redirect 192.0.2.9` | To a VRF (route target), or to a next-hop |
| `redirect-to-nexthop` | `redirect-to-nexthop` | |
| `copy` | `copy 192.0.2.9` | |
| `mark` | `mark 12` | DSCP |
| `action` | `action sample-terminal` | `sample`, `terminal`, or both |
| `community`, `large-community`, `extended-community` | `community [ 65000:1 ]` | As for routes |

For example, to drop a DNS amplification attack on one of your addresses, on
a session that carries `IPV4/FLOWSPEC`:

```bash
echo "announce flow route destination 203.0.113.7/32 protocol udp source-port =53 packet-length >=512 discard" |
bgpipe -- speaker --active --asn 65001 --id 192.0.2.2 \
    -- stdin --format exa --wait ESTABLISHED \
    -- connect 192.0.2.1
```

bgpipe keeps the session up after its input ends, and the rule with it: stop
bgpipe to remove the rule, or send a `withdraw flow route` with the same match.

See [Flowspec JSON format](flowspec.md) for the same rules in JSON.

## Feed an ExaBGP instance

To have ExaBGP announce routes taken from an MRT file (or any other bgpipe
input), convert them to commands first:

```bash
bgpipe -- read updates.mrt.gz -- grep 'prefix ~ 203.0.113.0/24' -- write routes.exa
```

Then print the file from an ExaBGP process. The script must keep running, as
ExaBGP restarts a process that exits:

```bash
#!/bin/sh
cat /etc/exabgp/routes.exa
while sleep 3600; do :; done
```

```
process routes {
    run /etc/exabgp/routes.sh;
    encoder text;
}
```

## See Also

[exec](stages/exec.md),
[update](stages/update.md),
[Flowspec](flowspec.md),
[JSON format](json-format.md),
[Examples](examples.md)
