# Docker

bgpipe is published as a minimal, multi-architecture Docker image at `ghcr.io/bgpfix/bgpipe`.

Available tags:

- `latest` - latest build from `main` branch
- `vX.Y.Z` - specific release
- `vX.Y` - latest patch of a minor release

## Quick Start

```bash
# print help
docker run --rm ghcr.io/bgpfix/bgpipe:latest --help

# find RPKI-invalid announcements in RIPE RIS live stream
docker run --rm ghcr.io/bgpfix/bgpipe:latest -go \
    -- ris-live \
    -- rpki --invalid=keep \
    -- grep 'tag[rpki/status] == INVALID'
```

## Reading Files from the Host

Mount a host directory to pass MRT files or capture output:

```bash
# read a local MRT file and write JSON output to the host
docker run --rm \
    -v /path/to/data:/data \
    ghcr.io/bgpfix/bgpipe:latest \
    -- read /data/updates.mrt \
    -- write /data/output.json
```

## BGP Sessions (Port Forwarding)

To run bgpipe as a proxy accessible from the host or other containers, expose port 179:

```bash
# transparent proxy: host:1790 -> bgpipe -> 192.0.2.1:179
docker run --rm \
    -p 1790:179 \
    ghcr.io/bgpfix/bgpipe:latest \
    -- listen :179 \
    -- connect --wait "listen" 192.0.2.1
```

`--wait listen` tells the `connect` stage to wait until `listen` has accepted a connection before dialling out. This ensures the two halves of the proxy session are always synchronized.

## Docker Compose Examples

[Docker Compose](https://docs.docker.com/compose/) lets you define multi-container setups in a
single YAML file. To run any example below, save it as `compose.yml` in a new directory and run:

```bash
docker compose up       # start (Ctrl+C to stop)
docker compose down     # clean up
```

### RIS Live Monitoring

The simplest example to try - no files or routers needed. Streams live BGP from [RIPE RIS Live](https://ris-live.ripe.net/) and prints matching routes to stdout:

```yaml
services:
  bgpipe:
    image: ghcr.io/bgpfix/bgpipe:latest
    command: >-
      -g
      -- ris-live
      -- grep 'prefix ~ 8.0.0.0/8'
      -- stdout
```

### MRT to JSON

Fetch a live MRT file from RIPE RIS and write it as JSON. The `read` stage handles URLs and gzip decompression automatically; the output JSON lands on the host via a volume mount:

```yaml
services:
  bgpipe:
    image: ghcr.io/bgpfix/bgpipe:latest
    volumes:
      - ./data:/data
    command: >-
      -- read https://data.ris.ripe.net/rrc01/2025.11/updates.20251107.2300.gz
      -- write /data/output.json
```

```bash
mkdir data
docker compose up
# output.json appears in ./data/ when done
```

### RPKI Proxy with Routinator

Run bgpipe as a RPKI-validating BGP proxy. [Routinator](https://routinator.docs.nlnetlabs.nl/) is an open-source RPKI validator by NLnet Labs that bgpipe connects to over RTR.

```yaml
services:
  routinator:
    image: nlnetlabs/routinator:latest
    command: server --rtr 0.0.0.0:3323 --http 0.0.0.0:8323

  bgpipe:
    image: ghcr.io/bgpfix/bgpipe:latest
    command: >-
      -- listen :179
      -- rpki --rtr routinator:3323
      -- connect --wait listen 192.0.2.1
    ports:
      - "1790:179"
    depends_on:
      - routinator
```

Replace `192.0.2.1` with the address of your downstream router. bgpipe listens on port 1790 on the host, accepts one BGP connection, and proxies it through RPKI validation before forwarding to the real router.

## Building Locally

The Dockerfile auto-detects the target platform, so a plain `docker build` produces the right image for your machine — no flags needed:

```bash
git clone https://github.com/bgpfix/bgpipe
cd bgpipe
docker build -t bgpipe .
docker run --rm bgpipe --help
```

To explicitly target a different platform:

```bash
docker build --platform linux/arm64 -t bgpipe .
```
