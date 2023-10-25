# bgpipe: a BGP reverse proxy

**WORK IN PROGRESS PREVIEW 10/2023**

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

```
# install golang, eg. https://go.dev/dl/
$ go version
go version go1.21.1 linux/amd64

# install bgpipe
$ go install github.com/bgpfix/bgpipe@latest

# bgpipe has built-in docs
$ bgpipe -h
Usage: bgpipe [OPTIONS] [--] STAGE [STAGE-OPTIONS] [STAGE-ARGUMENTS...] [--] ...

Options:
  -l, --log string       log level (debug/info/warn/error/disabled) (default "info")
  -D, --debug            alias for --log debug
  -e, --events strings   log given pipe events (asterisk means all) (default [PARSE,ESTABLISHED])
  -i, --stdin            read stdin after session is established (unless explicitly configured)
  -s, --silent           do not write stdout (unless explicitly configured)
  -r, --reverse          reverse the pipe
  -2, --short-asn        use 2-byte ASN numbers

Supported stages (run stage -h to get its help)
  connect                connect to a TCP endpoint
  exec                   pass through a background JSON processor
  listen                 wait for a TCP client to connect
  mrt                    read MRT file with BGP4MP messages (uncompress if needed)
  speaker                run a simple local BGP speaker
  stdin                  read JSON representation from stdin
  stdout                 print JSON representation to stdout

# see docs for "connect" stage
$ bgpipe connect -h
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
```

## Author

Pawel Foremski [@pforemski](https://twitter.com/pforemski) 2023
