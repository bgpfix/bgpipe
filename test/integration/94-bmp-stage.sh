#!/usr/bin/env bash
# the bmp stage as an exporter: bgpipe turns an MRT file into a BMP session
# (Initiation, Peer Up per peer, Route Monitoring) that Rotonda accepts
. "$(dirname "$0")/lib.sh"
need_docker
command -v curl >/dev/null 2>&1 || skip "curl not available"

run_daemon rotonda 11019 -p 127.0.0.1:0:8080 \
	-v "$TESTDATA/rotonda.conf:/etc/rotonda/rotonda.conf:ro" \
	nlnetlabs/rotonda:latest
API="http://127.0.0.1:$(daemon_port 8080)/api/v1"
wait_cmd 30 "docker logs $DAEMON 2>&1 | grep -q 'bmp-in: Listening'"
wait_cmd 30 "curl -sf $API/ingresses"

# a FIFO keeps the BMP session up: a station drops the routes when it ends
mkfifo "$WORK/in.mrt"
run_bgpipe -- read --format mrt "$WORK/in.mrt" -- bmp --write --sys-name bgpipe-test "127.0.0.1:$PORT"
exec 9>"$WORK/in.mrt"
cat "$TESTDATA/sample.mrt" >&9

# the router, and all 26 peers of the MRT file
wait_cmd 20 "curl -sf $API/ingresses | jq -e '[.data[] | select(.ingress_type==\"bmp\" and .name==\"bgpipe-test\")] | length == 1'"
wait_cmd 20 "curl -sf $API/ingresses | jq -e '[.data[] | select(.ingress_type==\"bgpViaBmp\")] | length == 26'"

# an IPv4 and an IPv6 route, with the peer they came from
wait_cmd 20 "curl -sf $API/ribs/ipv4unicast/routes/182.239.53.0/24 | jq -e '[.data.routes[].ingress.remote_asn] | index(2914)'"
n=$(curl -sf "$API/ribs/ipv6unicast/routes" | jq '.included.moreSpecifics | length')
[ "$n" -gt 0 ] || fail "no IPv6 routes in Rotonda"
msg "Rotonda has $n IPv6 prefixes"
