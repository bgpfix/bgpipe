# bgpipe: BGP reverse proxy and firewall

**bgpipe** is an open-source tool for processing messages exchanged by [the Border Gateway Protocol (BGP)](https://en.wikipedia.org/wiki/Border_Gateway_Protocol), which is [the routing protocol that makes the Internet work](https://learn.nsrc.org/bgp/bgp_intro).

**bgpipe** serves as a universal proxy sitting between BGP routers, capable of auditing, fixing, and securing BGP sessions on the fly.
It is based on the [BGPFix library](https://bgpfix.org/), distributed under the MIT license, and implemented in [Go](https://en.wikipedia.org/wiki/Go_(programming_language)), making it widely available for many platforms.

Started in 2023 and currently in beta, bgpipe [has its roots](https://dl.acm.org/doi/10.1145/3634737.3657000) in a research project developed at [the Institute of Theoretical and Applied Informatics, Polish Academy of Sciences](https://www.iitis.pl/en).

<div class="grid cards" markdown>

-   :material-book:{ .lg .middle } __Dive in!__

    Read how bgpipe works and how to use it

    [:octicons-arrow-right-24: Introduction](intro.md)

-   :simple-github:{ .lg .middle } __Free & Open-Source__

    Browse the source code repository

    [:octicons-arrow-right-24: GitHub Project](https://github.com/bgpfix/bgpipe)

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

## Examples

```bash linenums="1"
# connect to a BGP speaker, respond to OPEN, dump to JSON
$ bgpipe -o speaker 1.2.3.4

# JSON to BGP and back
$ cat input.json | bgpipe -io speaker 1.2.3.4 | tee output.json

# dump MRT updates to JSON
$ bgpipe read --mrt updates.20230301.0000.bz2 -- write output.json

# proxy a connection, print the conversation to stdout by default
# 1st stage: listen on TCP *:179 for new connection
# 2nd stage: wait for new connection and proxy it to 1.2.3.4, adding TCP-MD5
$ bgpipe -o \
	-- listen :179 \
	-- connect --wait listen --md5 solarwinds123 1.2.3.4

# a BGP speaker that streams an MRT file
# 1st stage: active BGP speaker for AS65055
# 2nd stage: MRT file reader, starting when the BGP session is established
# 3rd stage: listen on TCP *:179 for new connection
$ bgpipe \
  -- speaker --active --asn 65055 \
  -- read --mrt --wait ESTABLISHED updates.20230301.0000.bz2 \
  -- listen :179

# a BGP sed-in-the-middle proxy rewriting ASNs in OPEN messages
$ bgpipe \
  -- connect 1.2.3.4 \
  -- exec -LR --args sed -ure '/"OPEN"/{ s/65055/65001/g; s/57355/65055/g }' \
  -- connect 85.232.240.179

# filter prefix lengths and add max-prefix session limits
$ bgpipe --kill limit/session \
  -- connect 1.2.3.4 \
  -- limit -LR --ipv4 --min-length  8 --max-length 24 --session 1000000 \
  -- limit -LR --ipv6 --min-length 16 --max-length 48 --session 250000 \
  -- connect 5.6.7.8

# stream a log of BGP session in JSON to a remote websocket
$ bgpipe \
  -- connect 1.2.3.4 \
  -- websocket -LR --write wss://bgpfix.com/archive?user=demo \
  -- connect 85.232.240.179

# proxy a connection dropping non-IPv4 updates
$ bgpipe \
  -- connect 1.2.3.4 \
  -- grep -v --ipv4 \
  -- connect 85.232.240.179
```
