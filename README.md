# bgpipe: a BGP reverse proxy

An open-source BGP reverse proxy, firewall, and traffic processor based on [the BGPFix library](https://github.com/bgpfix/bgpfix).

**📖 Full documentation at [bgpipe.org](https://bgpipe.org/)**

## What is bgpipe?

bgpipe works as a **pipeline of data processing stages** that slice and dice streams of BGP messages. It can be used as a powerful BGP firewall that transparently secures, enhances, and audits existing BGP speakers.

**Use cases:**
- BGP man-in-the-middle proxy with full conversation logging and manipulation
- Bidirectional BGP to JSON translation for filtering and monitoring (supports Flowspec)
- Secure BGP transport over websockets + TLS
- MRT file processing and conversion (to/from JSON)
- Fast BGP packet filtering with custom rules
- Prefix length and count limits enforcement
- Router control plane firewall (drop, modify, and synthesize BGP messages)
- Replace [ExaBGP](https://github.com/Exa-Networks/exabgp) for faster performance and lower resource usage

The vision is to bolster open source innovation in the closed world of big BGP router vendors. See the [RIPE 88 bgpipe talk](https://ripe88.ripe.net/archives/video/1365/) for background.

## Quick Example

```bash
# Reverse proxy: expose internal BGP router, log all traffic to JSON
bgpipe --stdout \
  -- listen :179 \
  -- connect --wait listen 192.0.2.1

# Stream MRT file, filter by matching IP prefix, and store as JSON file
bgpipe \
  -- read https://data.ris.ripe.net/rrc01/2025.11/updates.20251107.2300.gz \
  -- grep 'prefix ~ 198.41.0.4' \
  -- write output.json
```

## Installation

Download pre-built binaries from [GitHub Releases](https://github.com/bgpfix/bgpipe/releases/latest), or install from source:

```bash
go install github.com/bgpfix/bgpipe@latest
```

Run `bgpipe -h` or `bgpipe <stage> -h` for built-in help.

## Documentation

Visit **[bgpipe.org](https://bgpipe.org)** for:
- [Quick start guide](https://bgpipe.org/quickstart/)
- [Examples and tutorials](https://bgpipe.org/examples/)
- [Filter reference](https://bgpipe.org/filters/)
- Complete documentation

## Author

Pawel Foremski [@pforemski](https://twitter.com/pforemski) 2023-2025
