#!/usr/bin/env bash
# GoBGP dials in to bgpipe listen; bgpipe speaker answers passively
. "$(dirname "$0")/lib.sh"
need_docker

freeport
host_ip
render gobgp-active.conf.in gobgp.toml

# NB: bind 0.0.0.0 so the docker VM/bridge can reach us (macOS: docker runs in a VM)
run_bgpipe --stdout -- listen "0.0.0.0:$LPORT" -- speaker --asn 65000 --id 10.0.0.3

run_daemon gobgp - \
	--add-host=hostgw:host-gateway \
	-v "$WORK/gobgp.toml:/config/gobgp.toml:ro" \
	jauderho/gobgp:latest

# session up: GoBGP's OPEN arrives
wait_json 30 '.[3]=="OPEN" and .[0]=="L"'

# announce a route from GoBGP, expect it in bgpipe output
docker exec "$DAEMON" gobgp global rib add 10.3.0.0/24 origin igp
wait_prefix 20 "10.3.0.0/24"
