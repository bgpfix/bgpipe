# bgpipe examples

The **bgpipe** command-line tool processes and filters BGP messages through a series of stages. Each stage performs a specific action, such as connecting to a BGP speaker, reading MRT files, or filtering BGP messages.

Stages are specified in sequence, usually separated by `--`, and each stage can have its own set of options provided using flags. Global options that affect the entire bgpipe process are specified before the first stage.

Below are practical examples to help you get started with **bgpipe**.

## Connect to a BGP speaker

Connect to a BGP speaker at IP address `1.2.3.4`. The command responds to the OPEN message from `1.2.3.4` (using the same ASN) and dumps the session in JSON format to stdout (the `-o` switch). It's useful for debugging and monitoring BGP sessions, allowing you to see the raw BGP messages.

```bash
bgpipe -o speaker 1.2.3.4
```

## JSON to BGP and back

Convert a JSON input file to BGP messages, send them to a BGP speaker, and capture the output back in JSON format. This example is useful for testing BGP message processing in remote speakers.

```bash
cat input.json \
  | bgpipe -io speaker 1.2.3.4 \
  | tee output.json
```

## Convert MRT files to JSON

Read MRT updates from a compressed file and convert the updates to JSON format. This is particularly useful for analyzing historical BGP data stored in MRT files, which are often used for archiving BGP updates (e.g., see [RouteViews.org](https://www.routeviews.org/)).

```bash
bgpipe \
  -- read --mrt updates.20230301.0000.bz2 \
  -- write output.json
```

## Adding TCP-MD5

Set up a proxy that listens on TCP port 179, waits for a connection, and then proxies it to `1.2.3.4` with a [popular](https://www.theregister.com/2020/12/16/solarwinds_github_password/) TCP-MD5 password. The conversation is printed to stdout. This setup is useful for "securing" BGP sessions, ensuring that only authorized peers can establish a TCP connection. It supports multi-hop scenarios.

```bash
bgpipe -o \
  -- listen :179 \
  -- connect --wait listen --md5 solarwinds123 1.2.3.4
```

## Stream MRT files to BGP routers

Listen for new connections on TCP port 179. Configure an active BGP speaker for `AS65055` that streams a given MRT file when the BGP session is established. This example demonstrates how to replay historical BGP data in a live BGP session, which can be useful for testing and analysis.

```bash
bgpipe \
  -- speaker --active --asn 65055 \
  -- read --mrt --wait ESTABLISHED updates.20230301.0000.bz2 \
  -- listen :179
```

## BGP sed-in-the-middle proxy

Create a BGP proxy that connects `1.2.3.4` with [85.232.240.179](https://lukasz.bromirski.net/post/bgp-w-labie-3/), but rewrites ASNs in their OPEN messages using [sed](https://en.wikipedia.org/wiki/Sed). This is useful for quickly testing and modifying BGP sessions on the fly, allowing you to simulate different network scenarios.

```bash
bgpipe \
  -- connect 1.2.3.4 \
  -- exec -LR --args sed -ure '/"OPEN"/{ s/65055/65001/g; s/57355/65055/g }' \
  -- connect 85.232.240.179
```

## Applying prefix limits

Filter BGP updates based on prefix lengths and enforce maximum prefix session limits for both IPv4 and IPv6 connections. This helps in managing and securing BGP sessions by limiting the number of prefixes, which can prevent [resource exhaustion](https://kirin-attack.github.io/).

```bash
bgpipe --kill limit/session \
  -- connect 1.2.3.4 \
  -- limit -LR --ipv4 --min-length  8 --max-length 24 --session 1000000 \
  -- limit -LR --ipv6 --min-length 16 --max-length 48 --session 250000 \
  -- connect 5.6.7.8
```

## Archive BGP sessions over encrypted WebSockets

Stream the BGP session log in JSON format to a remote WebSocket server for real-time monitoring and archiving. This is useful for integrating BGP session data with external monitoring systems, providing a live feed of BGP activity.

```bash
bgpipe \
  -- connect 1.2.3.4 \
  -- websocket -LR --write wss://bgpfix.com/archive?user=demo \
  -- connect 85.232.240.179
```

## Grep for BGP messages in live sessions

Proxy a connection between two BGP peers, dropping all IPv4 updates. This is useful for environments that wish to only support IPv6 on specific sessions. The `grep` stage allows for complex filtering based on various criteria such as message type, prefix, ASN, and more.

```bash
bgpipe \
  -- connect 1.2.3.4 \
  -- grep -v --ipv4 \
  -- connect 85.232.240.179
```
