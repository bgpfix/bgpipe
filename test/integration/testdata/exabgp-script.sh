#!/bin/sh
# an ExaBGP-style API script: prints commands, as for an ExaBGP "process"
echo "announce route 10.97.0.0/24 next-hop 192.0.2.1 community [65000:1 no-export]"
echo "announce route 10.97.1.0/24 next-hop 192.0.2.1"
sleep 3
echo "withdraw route 10.97.1.0/24 next-hop 192.0.2.1"
sleep 3600
