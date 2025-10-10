# bgpipe: BGP reverse proxy and firewall

An open-source BGP reverse proxy, firewall, and traffic processor based on [the BGPFix library](https://github.com/bgpfix/bgpfix).

**📖 Full documentation at [bgpipe.org](https://bgpipe.org)**

## What is bgpipe?

bgpipe works as a **pipeline of data processing stages** that slice and dice streams of BGP messages. It can be used as a powerful BGP firewall that transparently secures, enhances, and audits existing BGP speakers.

**Use cases:**
- BGP man-in-the-middle proxy with full conversation logging
- BGP to JSON bridge for filtering and monitoring
- Secure BGP transport over websocket + TLS
- MRT file processing and conversion (to/from JSON)
- Fast BGP packet filtering with custom rules
- Prefix length and count limits enforcement
- Router control plane firewall (drop, modify, and synthesize BGP messages)

The vision is to bolster open source innovation in the closed world of big BGP router vendors. See the [RIPE 88 bgpipe talk](https://ripe88.ripe.net/archives/video/1365/) for background.

## Quick Example

```bash
# Proxy a BGP session and log all traffic to JSON
bgpipe -o \
  -- listen :179 \
  -- connect --wait listen 192.0.2.1

# Convert MRT file to JSON with filtering
bgpipe \
  -- read --mrt updates.20250601.0400.bz2 \
  -- grep 'prefix ~ 8.8.8.8' \
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
