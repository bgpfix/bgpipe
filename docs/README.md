# bgpipe: BGP reverse proxy and firewall

bgpipe is an open source tool for processing BGP messages, which works as a proxy between two BGP peers that can inspect, modify, and secure BGP sessions on the fly.
It is based on [the BGPFix library](https://bgpfix.org/), is available for many platforms, and distributed under the MIT license.

### Features

 * transparent, man-in-the-middle proxy between 2 BGP speakers
 * full, bi-directional BGP to JSON and MRT translation
 * filtering BGP sessions through a background process, eg. a Python script
 * archival of BGP sessions to a file or to a background process
 * remote processing over encrypted websockets (HTTPS), eg. in cloud
 * support for [many popular BGP RFCs](https://github.com/bgpfix/bgpfix/#bgp-features), including Flowspec
 * TCP server and client with TCP-MD5
 * out-of-the-box BGP session filters and limiters

### Introduction

For a video introduction,
see [the RIPE88 bgpipe talk](https://ripe88.ripe.net/archives/video/1365/),
or read its [APNIC blog summary](https://blog.apnic.net/2024/06/11/routing-topics-at-ripe-88/).
<video preload="metadata" style="width: 100%;" controls="" poster="https://ripe88.ripe.net/wp-content/themes/fluida-plus/images/webcast.jpg">
    <source type="video/mp4" src="https://ripe88.ripe.net/archive/video/pawel-foremski_bgp-pipe-open-source-bgp-reverse-proxy_side_20240523-140239.mp4">
</video>
