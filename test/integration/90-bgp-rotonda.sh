#!/usr/bin/env bash
# bgpipe dials NLnet Labs Rotonda (bgp-tcp-in, a strict RFC 4271 FSM), announces
# and withdraws routes; Rotonda's RIB is checked over its HTTP API
. "$(dirname "$0")/lib.sh"
need_docker
command -v curl >/dev/null 2>&1 || skip "curl not available"

run_daemon rotonda 11179 -p 127.0.0.1:0:8080 \
	-v "$TESTDATA/rotonda.conf:/etc/rotonda/rotonda.conf:ro" \
	nlnetlabs/rotonda:latest
API="http://127.0.0.1:$(daemon_port 8080)/api/v1"
wait_cmd 30 "docker logs $DAEMON 2>&1 | grep -q 'bgp-in: Listening'"
wait_cmd 30 "curl -sf $API/ingresses"

# rib <prefix> - prints the number of active routes Rotonda has for prefix
rib() {
	curl -sf "$API/ribs/ipv4unicast/routes/$1" | jq '[.data.routes[]? | select(.status=="active")] | length'
}

# feed from a FIFO to keep the session alive throughout the test
# NB: --wait ESTABLISHED is a must here: Rotonda answers anything but a
# KEEPALIVE in OpenConfirm with a NOTIFICATION (FSM error), as RFC 4271 says
mkfifo "$WORK/in.json"
run_bgpipe --stdout -- speaker --active --asn 65010 --id 10.0.0.2 \
	-- read --wait ESTABLISHED "$WORK/in.json" \
	-- connect "127.0.0.1:$PORT"
exec 9>"$WORK/in.json"
wait_json 20 '.[3]=="OPEN" and .[0]=="L"'
wait_json 20 '.[3]=="KEEPALIVE" and .[0]=="L"'

# announce: all 3 prefixes must show up in Rotonda's RIB, with our AS_PATH
cat "$TESTDATA/updates.json" >&9
for p in 192.0.2.0/24 198.51.100.0/24 10.99.0.0/16; do
	wait_cmd 20 "[ \"\$(rib $p)\" = 1 ]"
done
aspath=$(curl -sf "$API/ribs/ipv4unicast/routes/192.0.2.0/24" |
	jq -c '.data.routes[0].pathAttributes[] | select(.asPath) | .asPath')
[ "$aspath" = '["AS65010","AS65001"]' ] || fail "unexpected AS_PATH in Rotonda: $aspath"
peer=$(curl -sf "$API/ingresses" | jq -c '.data[] | select(.ingress_type=="bgp") | [.state, .remote_asn]')
[ "$peer" = '["Connected",65010]' ] || fail "unexpected peer state in Rotonda: $peer"

# withdraw one prefix: it must leave the RIB, the others must stay
echo '{"unreach": ["198.51.100.0/24"]}' >&9
wait_cmd 20 "[ \"\$(rib 198.51.100.0/24)\" = 0 ]"
[ "$(rib 192.0.2.0/24)" = 1 ] || fail "192.0.2.0/24 gone after an unrelated withdrawal"

# Rotonda must not have complained
if jq -e 'select(.[3]=="NOTIFY")' <"$WORK/out.json" >/dev/null 2>&1; then
	fail "Rotonda sent a NOTIFICATION"
fi
