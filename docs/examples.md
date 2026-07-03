Below are practical examples to help you get started with  `bgpipe`  after you went through the [quickstart guide](quickstart.md). These examples demonstrate how to use  `bgpipe`  for various BGP-related tasks, such as connecting to BGP speakers, reading MRT files, filtering messages, and more. For stage details, see the [Stages overview](stages/index.md) and the per-stage docs.

## Connect to a BGP speaker

Stages: [speaker](stages/speaker.md), [connect](stages/connect.md), [stdout](stages/stdout.md)

Connect to a BGP speaker and respond to OPEN message using the same ASN. Note that if an IP address is used as a stage, it is a shorthand for `connect <ip>`. The command dumps the session in JSON format to stdout, since the `-o` option is enabled. It's useful for debugging and monitoring BGP sessions, allowing you to see the raw BGP messages.

```bash
bgpipe -o speaker -- 1.2.3.4
```

## JSON to BGP and back

Stages: [stdin](stages/stdin.md), [speaker](stages/speaker.md), [connect](stages/connect.md), [stdout](stages/stdout.md)

Convert a JSON input file to BGP messages, send them to a BGP speaker, and capture the output back in JSON format. This example is useful for testing BGP message processing in remote speakers.

```bash
cat input.json \
  | bgpipe -io speaker -- 1.2.3.4 \
  | tee output.json
```

## Convert MRT files to JSON

Stages: [read](stages/read.md), [write](stages/write.md)

Read MRT updates from a compressed file and convert the updates to JSON format. This is particularly useful for analyzing historical BGP data stored in MRT files, which are often used for archiving BGP updates.

```bash
bgpipe \
  -- read updates.20230301.0000.bz2 \
  -- write output.json
```

## Adding TCP-MD5

Stages: [listen](stages/listen.md), [connect](stages/connect.md), [stdout](stages/stdout.md)

Set up a proxy that listens on TCP port 179, waits for a connection, and then proxies it to `1.2.3.4` with a [popular](https://www.theregister.com/2020/12/16/solarwinds_github_password/) TCP-MD5 password. The conversation is printed to stdout. This setup is useful for "securing" BGP sessions, ensuring that only authorized peers can establish a TCP connection. It supports multi-hop scenarios.

```bash
bgpipe -o \
  -- listen :179 \
  -- connect --wait listen --md5 solarwinds123 1.2.3.4
```

## Transparent (TPROXY) proxy

Stages: [listen](stages/listen.md), [connect](stages/connect.md)

Insert bgpipe on the wire between two routers without touching either router's BGP configuration. Unlike the port-forwarding setup above, both `listen` and `connect` run with `--transparent` (Linux `IP_TRANSPARENT`/TPROXY), so the router captured by `listen` sees the far router's real IP, and vice versa -- bgpipe is invisible at the IP layer. The router only needs its traffic to TCP/179 redirected to the bgpipe host (via TPROXY, policy routing, or an inline bridge); no BGP reconfiguration needed. This is the recommended way to insert bgpipe as an RPKI/ASPA firewall in an existing eBGP session.

```bash
bgpipe \
  -- listen --transparent --md5 "solarwinds123" :179 \
  -- rov \
  -- connect --transparent --md5 "solarwinds123" --ttl 255 --wait listen 0.0.0.0
```

See [connect's transparent mode](stages/connect.md#transparent-mode) for how the captured TCP tuple is propagated between the two stages.

## Stream MRT files to BGP routers

Stages: [speaker](stages/speaker.md), [read](stages/read.md), [listen](stages/listen.md)

Listen for new connections on TCP port 179. Configure an active BGP speaker for `AS65055` that streams a given MRT file when the BGP session is established. This example demonstrates how to replay historical BGP data in a live BGP session, which can be useful for testing and analysis.

```bash
bgpipe \
  -- speaker --active --asn 65055 \
  -- read --wait ESTABLISHED updates.20230301.0000.bz2 \
  -- listen :179
```

## BGP sed-in-the-middle proxy

Stages: [connect](stages/connect.md), [exec](stages/exec.md)

Create a BGP proxy that connects `1.2.3.4` with [85.232.240.179](https://lukasz.bromirski.net/post/bgp-w-labie-3/), but rewrites ASNs in their OPEN messages using [sed](https://en.wikipedia.org/wiki/Sed). This is useful for quickly testing and modifying BGP sessions on the fly, allowing you to simulate different network scenarios.

```bash
bgpipe \
  -- connect 1.2.3.4 \
  -- exec -LR --args sed -ure '/"OPEN"/{ s/65055/65001/g; s/57355/65055/g }' \
  -- connect 85.232.240.179
```

## Applying prefix limits

Stages: [connect](stages/connect.md), [limit](stages/limit.md)

Filter BGP updates based on prefix lengths and enforce maximum prefix session limits for both IPv4 and IPv6 connections. This helps in managing and securing BGP sessions by limiting the number of prefixes, which can prevent [resource exhaustion](https://kirin-attack.github.io/).

```bash
bgpipe --kill limit/session \
  -- connect 1.2.3.4 \
  -- limit -LR --ipv4 --min-length  8 --max-length 24 --session 1000000 \
  -- limit -LR --ipv6 --min-length 16 --max-length 48 --session 250000 \
  -- connect 5.6.7.8
```

## Archive BGP sessions over encrypted WebSockets

Stages: [connect](stages/connect.md), [websocket](stages/websocket.md)

Stream the BGP session log in JSON format to a remote WebSocket server for real-time monitoring and archiving. This is useful for integrating BGP session data with external monitoring systems, providing a live feed of BGP activity.

```bash
bgpipe \
  -- connect 1.2.3.4 \
  -- websocket -LR --write wss://bgpfix.com/archive?user=demo \
  -- connect 85.232.240.179
```

## Grep for BGP messages in live sessions

Stages: [connect](stages/connect.md), [grep](stages/grep.md)

Proxy a connection between two BGP peers, allowing only IPv6 updates from origin AS `12345`. This is useful for environments that wish to only accept IPv6 prefixes from a specific ASN. The `grep` stage allows for [complex filtering](./filters.md) based on various criteria such as message type, prefix, AS_PATH, and more.

```bash
bgpipe \
  -- connect 1.2.3.4 \
  -- grep 'ipv6 && as_origin = 12345' \
  -- connect 85.232.240.179
```

## Monitor BGP prefixes in real-time

Stages: [ris-live](stages/ris-live.md), [grep](stages/grep.md), [stdout](stages/stdout.md)

Connect to [RIPE RIS Live](https://ris-live.ripe.net/) to stream real-time BGP updates from many route collectors, and filter for a specific prefix you're monitoring. RIS Live provides a view of the global BGP routing table without needing your own BGP connections - perfect for network security monitoring, research, and troubleshooting.

```bash
# Monitor all announcements for your network prefix
bgpipe -g \
  -- ris-live \
  -- grep 'prefix ~ 1.1.1.0/24' \
  -- stdout
```

## Stream live BGP with RPKI filtering

Stages: [ris-live](stages/ris-live.md), [rov](stages/rov.md), [update](stages/update.md), [write](stages/write.md)

Stream RIS Live UPDATEs and validate them against RPKI to detect and filter invalid route announcements in real-time. This combines global visibility with cryptographic validation to protect against [BGP hijacking](https://en.wikipedia.org/wiki/BGP_hijacking) and route leaks. The updates will not be modified on the BGP level (`--invalid=keep` flag), but will be tagged with `rov/status = INVALID`. The `update` stage is then used to add a community `123:456` to invalid updates for easy identification. Finally, the stream is saved to a file in JSON format.

```bash
# Real-time BGP monitoring with RPKI validation
bgpipe -g \
  -- ris-live \
  -- rov --invalid=keep \
  -- update --if 'tag[rov/status] = INVALID' --add-com 123:456 \
  -- write ris-rpki-updates.json
```

## Secure your BGP sessions with RPKI

Stages: [listen](stages/listen.md), [rov](stages/rov.md), [connect](stages/connect.md)

Add RPKI validation to a BGP proxy between two routers: one dials into bgpipe's `listen` stage, and bgpipe forwards the now-validated session onward to `5.6.7.8` via `connect`. Invalid prefixes are automatically moved to the withdrawn list (following [RFC 7606](https://datatracker.ietf.org/doc/html/rfc7606)), preventing propagation of unauthorized route announcements. RPKI uses cryptographic signatures to verify that an AS is authorized to originate a prefix - this protects against both malicious hijacks and configuration errors. The validator connects to Cloudflare's public RTR server by default (or you can point the global `--rpki` option at another server or a local ROA cache file).

```bash
# Secure 5.6.7.8 by validating routes from the router that dials in on :179
bgpipe \
  -- listen :179 \
  -- rov \
  -- connect 5.6.7.8
```

## Strict RPKI enforcement mode

Stages: [listen](stages/listen.md), [rov](stages/rov.md), [connect](stages/connect.md)

Enable strict mode to treat prefixes without any RPKI ROA the same as invalid prefixes. This aggressive stance only forwards messages to `5.6.7.8` where all announced prefixes have explicit RPKI authorization, dropping and logging any violations.

```bash
# Drop messages announcing INVALID and/or NOT_FOUND prefixes
bgpipe --events rov/dropped \
  -- listen :179 \
  -- rov --strict --invalid=drop --event dropped \
  -- connect 5.6.7.8
```

## Detect route leaks with ASPA

Stages: [listen](stages/listen.md), [rov](stages/rov.md), [aspa](stages/aspa.md), [connect](stages/connect.md)

While `rov` validates that the origin AS is authorized to announce a prefix, it can't catch a route leak where a valid prefix arrives over an AS_PATH that violates provider-customer relationships. Add the `aspa` stage on top of `rov` to verify the whole AS_PATH is valley-free using ASPA records. Tell it the relationship with `--role`: here the router connecting to `listen` is our customer, so `aspa` applies the customer-to-provider ("upstream") verification procedure to its announcements -- see the [role table](stages/aspa.md#multi-peer-feeds) for what each `--role` value means and which procedure it selects.

```bash
bgpipe \
  -- listen :179 \
  -- rov \
  -- aspa --role customer \
  -- connect 192.0.2.1
```

## Real-time invalid-route quarantine

Stages: [ris-live](stages/ris-live.md), [rov](stages/rov.md), [grep](stages/grep.md), [update](stages/update.md), [write](stages/write.md), [websocket](stages/websocket.md)

Tag, quarantine, and mirror only RPKI-invalid announcements in real time. This keeps a clean audit trail for responders while streaming live alerts to a remote monitor.

```bash
bgpipe -g \
  -- ris-live \
  -- rov --invalid keep \
  -- grep 'tag[rov/status] == INVALID' \
  -- update --add-com 65000:666 \
  -- write invalid-routes.$TIME.json \
  -- websocket --write wss://monitor.example.com/bgp
```

## Rate limiting and sampling BGP streams

Stages: [ris-live](stages/ris-live.md), [write](stages/write.md), [listen](stages/listen.md), [connect](stages/connect.md)

Protect downstream systems from [BGP update storms](https://www.cidr-report.org/as2.0/#Leak-Statistics) by rate limiting message flow, or sample high-volume feeds for statistical analysis. The `--rate-limit` flag delays messages to maintain a maximum rate (messages per second), while `--rate-sample` randomly samples messages when over the rate threshold, discarding excess messages. This is particularly useful when processing RIS Live feeds or during BGP convergence events.

```bash
# Sample RIS Live at max 100 updates/sec to avoid overwhelming storage
bgpipe -g \
  -- ris-live --rate-sample 100 \
  -- write sampled-updates.json

# Rate limit updates from 5.6.7.8 to 50 msg/sec (smooths bursts)
bgpipe \
  -- listen :179 \
  -- connect --rate-limit 50 5.6.7.8
```

## ExaBGP compatibility

Stages: [listen](stages/listen.md), [exec](stages/exec.md), [connect](stages/connect.md), [stdin](stages/stdin.md), [stdout](stages/stdout.md)

Use the `--format=exa` flag to read and write [ExaBGP](https://github.com/Exa-Networks/exabgp) line format instead of JSON. This allows integration with existing ExaBGP-based scripts and tools.

```bash
# Process BGP messages with an ExaBGP-compatible script
bgpipe \
  -- listen :179 \
  -- exec --format=exa -LR --args /path/to/script.py \
  -- connect 5.6.7.8

# Convert JSON to ExaBGP format
cat session.json | bgpipe stdin -- stdout --format=exa
```
