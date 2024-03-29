# bgpipe: a BGP reverse proxy

**WORK IN PROGRESS PREVIEW 02/2024**

This project provides an open-source BGP reverse proxy based on [the BGPFix library](https://github.com/bgpfix/bgpfix).

For example, bgpipe can be used to run:

 * a BGP man-in-the-middle proxy that dumps and controls all conversation
 * a bidirectional BGP to JSON bridge
 * a BGP listener on one end that connects adding (or changing) TCP-MD5 password on the other end
 * a speaker (or proxy) that streams an MRT file after the session is established
 * a fast MRT to JSON dumper (eg. for data analysis)
 
The vision for bgpipe is to be a powerful *BGP firewall* that transparently secures, enhances, and audits existing BGP speakers. The hope is to bolster open source innovation in the closed world of big BGP router vendors.

Under the hood, it works as a pipeline of data processing stages that slice and dice streams of BGP messages. See [BGPFix docs](https://github.com/bgpfix/bgpfix) for more background.

## Install and usage

See [bgpipe releases](https://github.com/bgpfix/bgpipe/releases/) on GitHub, or compile from source:

```
# install golang, eg. https://go.dev/dl/
$ go version
go version go1.22.0 linux/amd64

# install bgpipe
$ go install github.com/bgpfix/bgpipe@latest

# bgpipe has built-in docs
$ bgpipe -h
Usage: bgpipe [OPTIONS] [--] STAGE [STAGE-OPTIONS] [STAGE-ARGUMENTS] [--] ...

Options:
  -v, --version          print detailed version info and quit
  -l, --log string       log level (debug/info/warn/error/disabled) (default "info")
  -e, --events strings   log given events ("all" means all events) (default [PARSE,ESTABLISHED])
  -k, --kill strings     kill session on given events
  -i, --stdin            read stdin after session is established (unless explicitly configured)
  -s, --silent           do not write stdout (unless explicitly configured)
  -2, --short-asn        use 2-byte ASN numbers

Supported stages (run stage -h to get its help)
  connect                connect to a BGP endpoint over TCP
  exec                   filter messages through a background JSON processor
  limit                  limit prefix lengths and counts
  listen                 wait for a BGP client to connect over TCP
  mrt                    read MRT file with BGP4MP messages (uncompress if needed)
  speaker                run a simple local BGP speaker
  stdin                  read JSON representation from stdin
  stdout                 print JSON representation to stdout
  websocket              copy messages to remote JSON processor over websocket

# see docs for "connect" stage
$ bgpipe connect -h
Stage usage: connect [OPTIONS] ADDR

Description: connect to a BGP endpoint over TCP

Options:
      --timeout duration   connect timeout (0 means none) (default 1m0s)
      --md5 string         TCP MD5 password

Common Options:
  -A, --args               consume all arguments till --
  -R, --right              operate in the R direction
  -L, --left               operate in the L direction
  -W, --wait strings       wait for given event before starting
  -S, --stop strings       stop after given event is handled
  -I, --in string          where to inject new messages (default "next")
```

## Examples

```bash
# connect to a BGP speaker, respond to OPEN
$ bgpipe speaker 1.2.3.4

# bidir bgp to json
$ cat input.json | bgpipe --stdin speaker 1.2.3.4 | tee output.json

# dump mrt updates to json
$ bgpipe updates.20230301.0000.bz2 > output.json

# proxy a connection, print the conversation to stdout by default
# 1st stage: listen on TCP *:179 for new connection
# 2nd stage: wait for new connection and proxy it to 1.2.3.4, adding TCP-MD5
$ bgpipe \
	-- listen :179 \
	-- connect --wait listen --md5 solarwinds123 1.2.3.4

# a BGP speaker that streams an MRT file
# 1st stage: active BGP speaker for AS65055
# 2nd stage: MRT file reader, starting when the BGP session is established
# 3rd stage: listen on TCP *:179 for new connection
$ bgpipe \
  -- speaker --active --asn 65055 \
  -- mrt --wait ESTABLISHED updates.20230301.0000.bz2 \
  -- listen :179

# a BGP sed-in-the-middle proxy rewriting ASNs in OPEN messages
$ bgpipe \
  -- connect 1.2.3.4 \
  -- exec -LR -c sed -ure '/"OPEN"/{ s/65055/65001/g; s/57355/65055/g }' \
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
  -- websocket -LR wss://bgpfix.com/log?user=demo \
  -- connect 85.232.240.179
```

## Author

Pawel Foremski [@pforemski](https://twitter.com/pforemski) 2023
