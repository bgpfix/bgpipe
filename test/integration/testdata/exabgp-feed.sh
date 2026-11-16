#!/bin/sh
# ExaBGP API process: announce the routes one by one (else ExaBGP may reorder
# and merge them), then stay alive (ExaBGP expects that)
sleep 2
while read -r line; do
	echo "$line"
	sleep 0.2
done </etc/exabgp/routes.exa
sleep 3600
