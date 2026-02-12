# bgpipe: BGP pipeline processor

[![Docker Image](https://ghcr-badge.egpl.dev/bgpfix/bgpipe/size?label=docker)](https://github.com/bgpfix/bgpipe/pkgs/container/bgpipe)
[![GitHub Release](https://img.shields.io/github/v/release/bgpfix/bgpipe)](https://github.com/bgpfix/bgpipe/releases/latest)

An open-source tool that processes BGP messages through a pipeline of composable stages, built on [the bgpfix library](https://github.com/bgpfix/bgpfix).

**Full documentation at [bgpipe.org](https://bgpipe.org/)**

## What is bgpipe?

bgpipe sits between routers as a transparent proxy, auditing, filtering, and transforming BGP sessions on the fly. Think of it as a scriptable BGP firewall and traffic processor.

**Use cases:**
- BGP firewall with RPKI validation, prefix limits, and rate limiting
- Bidirectional BGP to JSON translation including Flowspec — pipe through jq, Python, anything
- MRT file processing and conversion at scale
- Scriptable pipeline — chain stages or pipe through external programs
- Live BGP monitoring from RIPE RIS Live or RouteViews with real-time filters
- Secure transport — add TCP-MD5 to sessions, proxy over encrypted WebSockets

See the [RIPE 88 bgpipe talk](https://ripe88.ripe.net/archives/video/1365/) for background.

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

## Author

Pawel Foremski [@pforemski](https://twitter.com/pforemski) 2023-2026
