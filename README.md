# bgpipe: a BGP reverse proxy

**WORK IN PROGRESS PREVIEW 10/2023**

This project provides a generic-purpose, MIT-licensed BGP reverse proxy based on [the BGPFix library](../bgpfix/) that can be used to run:

 * a BGP firewall
 * a BGP man-in-the-middle proxy that dumps all conversation to JSON
 * a BGP proxy that listens on one end and connects adding TCP-MD5 on the other
 * a BGP speaker (or proxy) that streams given MRT file after a session is established
 * a generic-purpose, bidirectional BGP to JSON bridge (eg. under a parent process)
 * a fast MRT updates to JSON dumper
 
## Examples

```
# bidir bgp to json
cat input.json | bgpipe -i speaker 1.2.3.4:179 | tee output.json

# dump mrt updates to json
bgpipe updates.20230301.0000.bz2 > output.json

```

## Author
Pawel Foremski [@pforemski](https://twitter.com/pforemski) 2023
