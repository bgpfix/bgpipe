# bgpipe: a BGP reverse proxy

**WORK IN PROGRESS PREVIEW 10/2023**

This project provides a generic-purpose, open-source BGP reverse proxy based on [the BGPFix library](https://github.com/bgpfix/bgpfix) that can be used to run:

 * a generic-purpose, bidirectional BGP to JSON bridge (eg. under a parent process)
 * a BGP man-in-the-middle proxy that dumps all conversation to JSON
 * a BGP listener on one end that connects adding TCP-MD5 on another
 * a speaker (or proxy) that streams given MRT file after a session is established
 * a fast MRT updates to JSON dumper
 
In overall, bgpipe implements a flexible *BGP firewall* that can transparently secure and enhance existing BGP speakers. It works as a pipeline of data processing stages that can slice and dice streams of BGP messages.

## Examples

```
# connect to a BGP speaker, respond to OPEN
bgpipe speaker 1.2.3.4:179

# bidir bgp to json
cat input.json | bgpipe -i speaker 1.2.3.4:179 | tee output.json

# dump mrt updates to json
bgpipe updates.20230301.0000.bz2 > output.json

# proxy a connection, print the conversation to stdout
bgpipe listen :179 tcp --md5 foobar 1.2.3.4:179
```

## Author
Pawel Foremski [@pforemski](https://twitter.com/pforemski) 2023
