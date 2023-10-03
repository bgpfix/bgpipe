# bgpipe: a BGP reverse proxy

**WORK IN PROGRESS PREVIEW 10/2023**

This project provides a generic-purpose, open-source BGP reverse proxy based on [the BGPFix library](https://github.com/bgpfix/bgpfix) that can be used to run:

 * a BGP man-in-the-middle proxy that dumps all conversation to JSON
 * a BGP proxy that listens on one end and connects adding TCP-MD5 on the other
 * a BGP speaker (or proxy) that streams given MRT file after a session is established
 * a generic-purpose, bidirectional BGP to JSON bridge (eg. under a parent process)
 * a fast MRT updates to JSON dumper
 
In overall, bgpipe implements a flexible *BGP session firewall* that can transparently secure and enhance existing BGP speakers.

## Examples

```
# bidir bgp to json
cat input.json | bgpipe -i speaker 1.2.3.4:179 | tee output.json

# dump mrt updates to json
bgpipe updates.20230301.0000.bz2 > output.json

```

## Author
Pawel Foremski [@pforemski](https://twitter.com/pforemski) 2023
