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
```

`ipv4 unicast` and `ipv6 unicast` are accepted in place of `route`. Empty lines
and lines starting with `#` are skipped. A leading neighbor selector
(`neighbor 192.0.2.1 announce route ...`) is accepted and ignored: a bgpipe
pipeline is one BGP session, so run one bgpipe per neighbor.

| Attribute | Example | Notes |
|-----------|---------|-------|
| `next-hop` | `next-hop 192.0.2.1` | An IPv4 next-hop for an IPv6 prefix is sent IPv4-mapped (`::ffff:192.0.2.1`), as ExaBGP does. See below for `self` |
| `origin` | `origin igp` | `igp`, `egp`, or `incomplete`; `igp` if not given |
| `as-path` | `as-path [ 65001 65002 ]` | Empty if not given |
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

## Differences from ExaBGP

- **`next-hop self`** leaves the next-hop unset. Add
  [update](stages/update.md) `--nexthop-self` (or `--nexthop4` / `--nexthop6`)
  after the stage that reads the commands. As in ExaBGP, an IPv6 route on an
  IPv4 session gets the IPv4-mapped address (`::ffff:192.0.2.2`).
- **The AS path is sent as given.** ExaBGP prepends its own ASN on eBGP
  sessions when the command has no `as-path`; with bgpipe, write the path in the
  command, or add [update](stages/update.md) `--prepend ASN`.
- **Other attributes are errors** (eg. `label`, `path-information`, `aigp`,
  `attribute`), as are other commands (`announce flow`, `announce eor`,
  `announce attributes ... nlri`, ...): bgpipe does not announce a route
  without what was asked for. Add `--pardon` to skip such lines instead. Use
  the [JSON format](json-format.md) when you need every attribute.
- **Attributes after `withdraw route PREFIX`** are accepted but not needed.
- **AS sets are flattened** into the AS sequence.
- **Output is one command per prefix**, with the attributes listed above. Other
  message types (OPEN, KEEPALIVE, End-of-RIB) have no command and are skipped.

## Run an ExaBGP script

An ExaBGP process script prints commands on its stdout. Run it with
[exec](stages/exec.md) and bgpipe announces the routes itself:

```bash
bgpipe \
    -- speaker --active --asn 65001 --id 192.0.2.2 \
    -- exec --format exa --read --wait ESTABLISHED /usr/local/bin/healthcheck.sh \
    -- update --nexthop-self --prepend 65001 \
    -- connect 192.0.2.1
```

- `--wait ESTABLISHED` starts the script once the session is up, so nothing is
  announced too early
- `--read` makes the stage read-only: bgpipe writes nothing to the script's
  stdin
- `--prepend 65001` is for eBGP; leave it out on an iBGP session

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
[JSON format](json-format.md),
[Examples](examples.md)
