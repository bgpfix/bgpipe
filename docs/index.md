---
description: "bgpipe is an open-source BGP reverse proxy and pipeline processor - filter, transform, and monitor live BGP sessions with RPKI validation, JSON translation, MRT processing, and many composable stages."
---

# bgpipe: BGP pipeline processor

**bgpipe** is an open-source tool that processes [BGP](https://en.wikipedia.org/wiki/Border_Gateway_Protocol) messages
through a pipeline of composable stages - [*bridging the gaps*](https://www.youtube.com/watch?v=Y-YCYXGF_UY) between monitoring and control.

Usually, bgpipe sits between routers as a transparent proxy, auditing, filtering, and transforming BGP sessions on the fly.
The same pipeline works on MRT archives, BMP feeds from your routers, and live streams from RIPE RIS and RouteViews.
Built on the [bgpfix](https://bgpfix.org/) library, written in [Go](https://go.dev/), and distributed under the MIT license.
Started in 2023 as part of a [research project](https://dl.acm.org/doi/10.1145/3634737.3657000) at the [Polish Academy of Sciences](https://www.iitis.pl/en).

```
  sources               processors                      sinks
  listen   ─┐                                       ┌─▶  connect
  read      │   grep · rov · aspa · limit · update  │    write
  ris-live  ├─▶   tag · exec · pipe · metrics     ──┤    stdout
  rv-live   │                                       │    websocket
  bmp      ─┘                                       └─▶  bmp --write
```

Each stage does one thing; chain them with `--`. Messages flow in both directions,
so the very same pipeline can sit inside a live BGP session.

<div class="grid cards" markdown>

-   :material-rocket-launch:{ .lg } __Quick Start__

    Get started in minutes<br>
    [:octicons-arrow-right-24: Quick Start](quickstart.md)

-   :material-list-box:{ .lg } __Examples__

    See bgpipe in action<br>
    [:octicons-arrow-right-24: Examples](examples.md)

-   :simple-github:{ .lg } __Downloads__

    Download and install bgpipe<br>
    [:octicons-arrow-right-24: GitHub Releases](https://github.com/bgpfix/bgpipe/releases)

-   :material-pipe:{ .lg } __Stages__

    Browse the building blocks<br>
    [:octicons-arrow-right-24: Stage Reference](stages/index.md)

</div>

## Quick Demo

Stream [live from RIPE RIS](https://ris-live.ripe.net/), add [RPKI validation](https://en.wikipedia.org/wiki/Resource_Public_Key_Infrastructure) on the fly, and show RPKI-invalid announcements:

```bash
$ docker run --rm ghcr.io/bgpfix/bgpipe:latest -o \
    -- ris-live \
    -- rov --invalid=keep \
    -- grep 'tag[rov/status] == INVALID'
```

Sample output:
```json
[
    "R",
    10843,
    "2026-02-10T12:47:02.900",
    "UPDATE",
    {
        "reach":["201.49.180.0/23","201.49.181.0/24"],
        "attrs":{
            "ORIGIN":{"flags":"T","value":"IGP"},
            "ASPATH":{"flags":"T","value":[199524,174,52320,53062,262907,262907,262907,262907,262907,52900,273801]},
            "NEXTHOP":{"flags":"T","value":"196.60.9.188"}
        }
    },
    {
        "PEER_IP":"196.60.9.188",
        "PEER_AS":"199524",
        "COLLECTOR":"rrc19",
        "RIS_HOST":"rrc19.ripe.net",
        "RIS_ID":"196.60.9.188-019c479748f40019",
        "rov/201.49.180.0/23":"INVALID",
        "rov/201.49.181.0/24":"INVALID",
        "rov/status":"INVALID"
    }
]
```

More one-liners:

```bash
# convert an MRT archive to JSON, straight from RIPE RIS (or RouteViews) URL
$ bgpipe read https://data.ris.ripe.net/rrc01/latest-update.gz | head

# stream a full RIB snapshot as BGP updates, keeping one peer's view
$ bgpipe -o -- read https://data.ris.ripe.net/rrc01/latest-bview.gz \
    -- grep 'tag[PEER_AS] == "8218"'

# proxy a BGP session, dump messages to stdout, withdraw RPKI-invalid prefixes
$ bgpipe -o -- listen --peer 198.51.100.7 :179 -- rov -- connect --wait listen 192.0.2.1

# BMP station: collect from your routers, keep post-policy routes, archive as MRT
$ bgpipe -- bmp --listen --peer 192.0.2.0/24 :11019 \
    -- grep 'tag[BMP_RIB] == in-post' -- write --every 15m 'bmp.$TIME.mrt.gz'

# turn RIPE RIS Live into a BMP feed for your own collector
$ bgpipe -- ris-live -- bmp --write collector.example.net:11019
```

## Install

=== "Docker"

    ```bash
    docker pull ghcr.io/bgpfix/bgpipe:latest
    ```

=== "Binary"

    ```bash
    wget -O bgpipe \
        https://github.com/bgpfix/bgpipe/releases/latest/download/bgpipe-linux-amd64
    chmod +x bgpipe
    ```

=== "Go install"

    ```bash
    go install github.com/bgpfix/bgpipe@latest
    ```

A single static binary, no dependencies - see the [Quick Start](quickstart.md) for details and other platforms.

## Use Cases

<div class="grid cards" markdown>

-   :material-shield-lock:{ .lg } **BGP Firewall**

    Drop-in proxy with RPKI ROV + ASPA validation, prefix limits, and rate limiting

-   :material-code-json:{ .lg } **Full JSON Translation**

    Bidirectional BGP <-> JSON including Flowspec - pipe through jq, Python, anything

-   :material-database:{ .lg } **MRT Processing**

    Read, convert, and filter compressed MRT archives at scale - update dumps and full RIB snapshots alike

-   :material-console:{ .lg } **Scriptable Pipeline**

    Chain stages or pipe messages through external programs

-   :material-earth:{ .lg } **Live BGP Monitoring**

    Stream from RIPE RIS Live or RouteViews with real-time filters

-   :material-radar:{ .lg } **BMP Station & Exporter**

    Collect BMP from your routers, or turn any session, archive, or feed into BMP

-   :material-lock:{ .lg } **Secure Transport**

    Add TCP-MD5 to sessions, proxy over encrypted WebSockets

-   :material-speedometer:{ .lg } **Fast & Hardened**

    ~1M messages/s, zero-allocation parsing, fuzzed parsers, and safe defaults

</div>

## Features

- **Fast** - UPDATE parsing in ~175 ns with zero allocations; ~1M messages/s from MRT to JSON on one box
- **Hardened** - all parsers are fuzzed: malformed input from a peer or a public feed can't crash the process
- **Safe by default** - peer allow-lists, authenticated servers, and slow clients can never stall a session
- **Interop-tested** - every change runs against GoBGP, BIRD, FRR, NLnet Labs Rotonda, and FastNetMon Netom
- **Transparent proxy** - with [TPROXY mode](stages/connect.md#transparent-mode), invisible even at the IP layer: no router reconfiguration
- **RPKI** - origin validation ([rov](stages/rov.md), RFC 6811) and route leak detection ([aspa](stages/aspa.md), IETF draft-29) - before router vendors ship it
- **Route policy** - prefix and rate [limits](stages/limit.md); AS_PATH, community, MED, LOCAL_PREF and ORIGIN rewrites with [update](stages/update.md)
- **Every format** - [JSON](json-format.md) (incl. [Flowspec](flowspec.md)), [MRT](mrt.md) updates and RIB dumps, BMP, OpenBMP, ExaBGP, and raw BGP; gzip/bzip2/zstd; files, URLs, and pipes
- **BMP** - a [station and exporter](stages/bmp.md) over TCP or TLS: ADD-PATH per address family, RIB views, Peer Up/Down events, and statistics as metrics
- **Glue** - a concise [filter language](filters.md), message [tags](stages/tag.md), external programs, WebSockets, TCP-MD5, and [config files](quickstart.md#config-files)
- **Observable** - live dashboard, Prometheus metrics, health checks, and pprof over the [HTTP API](http-api.md)
- **22 stages** - composable building blocks, each doing one thing well ([full list](stages/index.md))

## Project

bgpipe is hosted on [GitHub](https://github.com/bgpfix/bgpipe) under the MIT license,
built on the [bgpfix](https://bgpfix.org/) library.

- Watch the [RIPE 88 talk](https://ripe88.ripe.net/archives/video/1365/) for a 15-minute introduction
- Read the [Kirin attack paper](https://kirin-attack.github.io/) that started the project
- Report bugs and request features on [GitHub Issues](https://github.com/bgpfix/bgpipe/issues)
- For collaboration inquiries, contact [bgpipe@bgpipe.org](mailto:bgpipe@bgpipe.org)
