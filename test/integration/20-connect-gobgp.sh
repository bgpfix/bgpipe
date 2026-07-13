#!/usr/bin/env bash
# bgpipe dials GoBGP (passive, dynamic neighbor), announces come back as JSON
. "$(dirname "$0")/lib.sh"
need_docker

run_daemon gobgp 1790 \
	-v "$TESTDATA/gobgp-passive.conf:/config/gobgp.toml:ro" \
	jauderho/gobgp:latest
wait_tcp 127.0.0.1 "$PORT"

run_bgpipe --stdout -- speaker --active --asn 65000 --id 10.0.0.2 -- connect "127.0.0.1:$PORT"

# session up: GoBGP's OPEN arrives
wait_json 20 '.[3]=="OPEN" and .[0]=="L"'

# announce a route from GoBGP, expect it in bgpipe output
docker exec "$DAEMON" gobgp global rib add 10.2.0.0/24 origin igp
wait_prefix 20 "10.2.0.0/24"
