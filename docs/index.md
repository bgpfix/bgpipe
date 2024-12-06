# bgpipe: a BGP firewall

**bgpipe** is an open-source tool for processing and filtering messages exchanged by [the Border Gateway Protocol (BGP)](https://en.wikipedia.org/wiki/Border_Gateway_Protocol). BGP is [the routing protocol that makes the Internet work](https://learn.nsrc.org/bgp/bgp_intro), and thus it is considered to be [critical to the global economic prosperity and security](https://www.whitehouse.gov/wp-content/uploads/2024/09/Roadmap-to-Enhancing-Internet-Routing-Security.pdf).

**bgpipe** operates as a proxy sitting between BGP routers, capable of auditing, fixing, and securing BGP sessions on the fly.
It is based on the [BGPFix library](https://bgpfix.org/), distributed under the MIT license, and implemented in [Go](https://en.wikipedia.org/wiki/Go_(programming_language)), making it widely available for many platforms.

Started in 2023 and currently in beta, bgpipe [has its roots](https://dl.acm.org/doi/10.1145/3634737.3657000) in a research project developed at [the Institute of Theoretical and Applied Informatics, Polish Academy of Sciences](https://www.iitis.pl/en).

<div class="grid cards" markdown>

-   :simple-quicklook:{ .lg } __See examples__

    Get BGP pipeline ideas<br>
    [:octicons-arrow-right-24: Examples](examples.md)

<!---   :material-book:{ .lg } __Read the docs__

    Learn how bgpipe works and how to use it<br>
    [:octicons-arrow-right-24: Introduction](intro.md)//-->

-   :simple-github:{ .lg } __Downloads__

    See released versions<br>
    [:octicons-arrow-right-24: GitHub Releases](https://github.com/bgpfix/bgpipe)

</div>

## Features

- Works as a transparent man-in-the-middle proxy.
- Has full, bi-directional BGP to JSON translation.
- Can filter and archive BGP sessions through an external process, eg. a Python script.
- Supports remote processing over encrypted WebSockets (HTTPS), eg. in the cloud.
- Reads and writes MRT files (BGP4MP), optionally compressed.
- Can add and drop TCP-MD5 on multi-hop BGP sessions, independently on each side.
- Has built-in BGP message filters and session limiters.
- Supports [popular BGP RFCs](https://github.com/bgpfix/bgpfix/#bgp-features), including Flowspec.
