# bgpipe: BGP pipeline processor

**bgpipe** is an open-source tool that processes [BGP](https://en.wikipedia.org/wiki/Border_Gateway_Protocol) messages
through a pipeline of composable stages — [*bridging the gaps*](https://www.youtube.com/watch?v=Y-YCYXGF_UY) between monitoring and control.

Usually, bgpipe sits between routers as a transparent proxy, auditing, filtering, and transforming BGP sessions on the fly.
Built on the [bgpfix](https://bgpfix.org/) library, written in [Go](https://go.dev/), and distributed under the MIT license.
Started in 2023 as part of a [research project](https://dl.acm.org/doi/10.1145/3634737.3657000) at the [Polish Academy of Sciences](https://www.iitis.pl/en).

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

-   :material-bomb:{ .lg } __Research__

    Read background paper<br>
    [:octicons-arrow-right-24: Kirin Attack](https://kirin-attack.github.io/)

</div>

## Quick Demo

Read [live data from RIPE RIS](https://ris-live.ripe.net/), do real-time [RPKI validation](https://en.wikipedia.org/wiki/Resource_Public_Key_Infrastructure) using [Cloudflare RTR server](https://rpki.cloudflare.com/), and show the first RPKI-invalid announcement.

```bash
$ bgpipe -go \
    -- ris-live \
    -- rpki --invalid=keep \
    -- grep 'tag[rpki/status] == INVALID' \
    -- head -n 1
```

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
        "rpki/201.49.180.0/23":"INVALID",
        "rpki/201.49.181.0/24":"INVALID",
        "rpki/status":"INVALID",
    }
]
```

## Use Cases

<div class="grid cards" markdown>

-   :material-shield-lock:{ .lg } **BGP Firewall**

    Drop-in proxy with RPKI validation, prefix limits, and rate limiting

-   :material-code-json:{ .lg } **Full JSON Translation**

    Bidirectional BGP ↔ JSON including Flowspec — pipe through jq, Python, anything

-   :material-database:{ .lg } **MRT Processing**

    Read, convert, and filter compressed MRT archives at scale

-   :material-console:{ .lg } **Scriptable Pipeline**

    Chain stages or pipe messages through external programs

-   :material-earth:{ .lg } **Live BGP Monitoring**

    Stream from RIPE RIS Live or RouteViews with real-time filters

-   :material-lock:{ .lg } **Secure Transport**

    Add TCP-MD5 to sessions, proxy over encrypted WebSockets

</div>

---

Built on [bgpfix](https://bgpfix.org/) · MIT license
