# bgpipe: a BGP reverse proxy

**WORK IN PROGRESS PREVIEW 10/2023**

This project provides an open-source BGP reverse proxy based on [the BGPFix library](https://github.com/bgpfix/bgpfix) that can be used to run:

 * a bidirectional BGP to JSON bridge (eg. under a parent process)
 * a BGP man-in-the-middle proxy that dumps all conversation to JSON
 * a BGP listener on one end that connects adding (or changing) TCP-MD5 password on the other end
 * a speaker (or proxy) that streams to a peer an MRT file after the session is established
 * a fast MRT to JSON dumper (eg. for data analysis)
 
The vision for bgpipe is to be a powerful *BGP firewall* that transparently secures, enhances, and audits existing BGP speakers. The hope is to enable disruptive innovation in the closed world of big BGP router vendors.

Under the hood, it works as a pipeline of data processing stages that slice and dice streams of BGP messages.

## Install and usage

```bash
# install golang, eg. https://go.dev/dl/
$ go version
go version go1.21.1 linux/amd64

# install bgpipe
$ go install github.com/bgpfix/bgpipe

# bgpipe has built-in docs
$ bgpipe -h
Usage: bgpipe [OPTIONS] [--] STAGE [STAGE-OPTIONS] [STAGE-ARGUMENTS...] [--] ...

Options:
  -l, --log string       log level (debug/info/warn/error/disabled) (default "info")
  -D, --debug            alias for --log debug
  -e, --events strings   log given pipe events (asterisk means all) (default [PARSE,ESTABLISHED])
  -i, --stdin            read stdin (even if not explicitly requested)
  -s, --silent           do not write stdout (unless explicitly requested)
  -r, --reverse          reverse the pipe
  -2, --short-asn        use 2-byte ASN numbers

Supported stages (run stage -h to get its help)
  connect                connect to a TCP endpoint
  listen                 wait for a TCP client to connect
  mrt                    read MRT file with BGP4MP messages (uncompress if needed)
  speaker                run a simple local BGP speaker
  stdin                  read JSON representation from stdin
  stdout                 print JSON representation to stdout

# see docs for "connect" stage
Stage usage: connect [OPTIONS] TARGET

connect to a TCP endpoint

Options:
  -L, --left               operate in L direction
  -R, --right              operate in R direction
  -W, --wait strings       wait for given event before starting
  -S, --stop strings       stop after given event is handled
      --timeout duration   connect timeout (0 means none)
      --md5 string         TCP MD5 password

Events:
  connected           connection established
```

## Examples

```bash
# connect to a BGP speaker, respond to OPEN
$ bgpipe speaker 1.2.3.4

# bidir bgp to json
$ cat input.json | bgpipe --stdin speaker 1.2.3.4 | tee output.json

# dump mrt updates to json
$ bgpipe updates.20230301.0000.bz2 > output.json

# proxy a connection, print the conversation to stdout
# 1st stage: bind to TCP *:179
# 2nd stage: wait for connection, and proxy to 1.2.3.4:179 adding TCP-MD5
$ bgpipe \
	-- listen :179 \
	-- connect --wait listen/connected --md5 solarwinds123 127.0.0.1:1790

# a BGP speaker that streams MRT file after establish
# 1st stage: active BGP speaker (AS65055), starting when client connects
# 2nd stage: MRT file reader, starting after session is established
# 3rd stage: bind to TCP *:179
$ bgpipe \
    -- speaker --wait listen/connected --active --asn 65055 \
    -- mrt --wait established ../test/updates.20230301.0000.bz2 \
    -- listen :179
```

## Author
Pawel Foremski [@pforemski](https://twitter.com/pforemski) 2023
