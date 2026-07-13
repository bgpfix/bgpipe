#!/usr/bin/env bash
# BIRD dials in to bgpipe listen and exports two static routes
. "$(dirname "$0")/lib.sh"
need_docker

# tiny local image: alpine + bird (no maintained multi-arch image upstream)
docker build -q -t bgpipe-test-bird -f "$TESTDATA/bird.Dockerfile" "$TESTDATA" >/dev/null

freeport
host_ip
render bird.conf.in bird.conf

run_bgpipe --stdout -- listen "0.0.0.0:$LPORT" -- speaker --asn 65000 --id 10.0.0.4

run_daemon bird - \
	--add-host=hostgw:host-gateway \
	-v "$WORK/bird.conf:/etc/bird.conf:ro" \
	bgpipe-test-bird

wait_json 30 '.[3]=="OPEN" and .[0]=="L"'
wait_prefix 20 "10.40.0.0/24"
wait_prefix 20 "10.41.0.0/24"
