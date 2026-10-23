# bgpipe: BGP pipeline processor

[![Docker Image](https://ghcr-badge.egpl.dev/bgpfix/bgpipe/size?label=docker)](https://github.com/bgpfix/bgpipe/pkgs/container/bgpipe)
[![GitHub Release](https://img.shields.io/github/v/release/bgpfix/bgpipe)](https://github.com/bgpfix/bgpipe/releases/latest)

An open-source tool that processes BGP messages through a pipeline of composable stages, built on [the bgpfix library](https://github.com/bgpfix/bgpfix).

**Full documentation at [bgpipe.org](https://bgpipe.org/)**

## What is bgpipe?

bgpipe sits between routers as a transparent proxy, auditing, filtering, and transforming BGP sessions on the fly. Think of it as a scriptable BGP firewall and traffic processor.

**Use cases:**
- BGP firewall with RPKI ROV and ASPA route-leak detection, prefix limits, and rate limiting
- Route policy on the wire - prepend, strip private ASNs, scrub communities, set MED / LOCAL_PREF
- Bidirectional BGP to JSON translation including Flowspec - pipe through jq, Python, anything
- MRT file processing and conversion at scale, incl. RIB snapshots (table dumps)
- Scriptable pipeline - chain stages or pipe through external programs, keep it in a config file
- Live BGP monitoring from RIPE RIS Live or RouteViews with real-time filters
- Secure transport - add TCP-MD5 to sessions, proxy over encrypted WebSockets

**Fast and hardened:**
- ~1M BGP messages/s from MRT to JSON on one box; UPDATE parsing takes ~175 ns with zero allocations ([bgpfix](https://github.com/bgpfix/bgpfix) v0.23)
- all parsers are fuzzed, and malformed input from a peer or a public feed can't crash the process
- safe defaults: `listen --peer` allow-lists, authenticated WebSocket servers, slow clients can't stall a session, RPKI data over HTTPS

See the [RIPE 88 bgpipe talk](https://ripe88.ripe.net/archives/video/1365/) for background.

## Quick Example

```bash
# Reverse proxy: expose internal BGP router to one peer, log all traffic to JSON
bgpipe --stdout \
  -- listen --peer 198.51.100.7 :179 \
  -- connect --wait listen 192.0.2.1

# Stream MRT file, filter by matching IP prefix, and store as JSON file
bgpipe \
  -- read https://data.ris.ripe.net/rrc01/2025.11/updates.20251107.2300.gz \
  -- grep 'prefix ~ 198.41.0.4' \
  -- write output.json
```

## Installation

**Docker** (fastest):
```bash
docker pull ghcr.io/bgpfix/bgpipe:latest
docker run --rm ghcr.io/bgpfix/bgpipe:latest --help
```

**Binary**: download from [GitHub Releases](https://github.com/bgpfix/bgpipe/releases/latest).

**Go**: `go install github.com/bgpfix/bgpipe@latest`

Run `bgpipe -h` or `bgpipe <stage> -h` for built-in help.

## Documentation

Visit **[bgpipe.org](https://bgpipe.org)** for:
- [Quick start guide](https://bgpipe.org/quickstart/)
- [Examples and tutorials](https://bgpipe.org/examples/)
- [Filter reference](https://bgpipe.org/filters/)
- Complete stage documentation

## Contributing

- Report bugs and request features on [GitHub Issues](https://github.com/bgpfix/bgpipe/issues)
- For collaboration inquiries, contact [bgpipe@bgpipe.org](mailto:bgpipe@bgpipe.org)
- Integration tests against real BGP daemons (GoBGP, BIRD, FRR, StayRTR, rtrtr) live in [test/integration](test/integration/)

## Author

Pawel Foremski [@pforemski](https://twitter.com/pforemski) 2023-2026
