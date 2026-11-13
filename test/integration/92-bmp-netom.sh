#!/usr/bin/env bash
# FastNetMon Netom (bgp-tcp-in -> rib -> bmp-tcp-out): bgpipe announces routes
# over BGP, and a second bgpipe reads them back from Netom's BMP restream:
# first the initial table dump, then a live withdrawal
. "$(dirname "$0")/lib.sh"
need_docker
command -v curl >/dev/null 2>&1 || skip "curl not available"

run_daemon netom 11179 -p 127.0.0.1:0:8080 -p 127.0.0.1:0:11020 \
	-v "$TESTDATA/netom.conf:/etc/netom/netom.conf:ro" \
	"${NETOM_IMAGE:-ghcr.io/fastnetmon/netom:0.6.0}"
API="http://127.0.0.1:$(daemon_port 8080)/api/v1"
BMPPORT=$(daemon_port 11020)
wait_cmd 30 "docker logs $DAEMON 2>&1 | grep -q 'bgp-in: Listening'"
wait_cmd 30 "docker logs $DAEMON 2>&1 | grep -q 'bmp-out: Listening'"
wait_cmd 30 "curl -sf $API/ingresses"

# BGP feeder: a FIFO keeps the session alive throughout the test
mkfifo "$WORK/in.json"
msg "bgpipe (feeder) -> 127.0.0.1:$PORT"
"$BGPIPE" --log warn -- speaker --active --asn 65010 --id 10.0.0.2 \
	-- read --wait ESTABLISHED "$WORK/in.json" \
	-- connect "127.0.0.1:$PORT" >"$WORK/feeder.json" 2>"$WORK/feeder.log" &
PIDS="$PIDS $!"
exec 9>"$WORK/in.json"

# announce, wait for Netom's RIB
cat "$TESTDATA/updates.json" >&9
for p in 192.0.2.0/24 198.51.100.0/24 10.99.0.0/16; do
	wait_cmd 20 "curl -sf $API/ribs/ipv4unicast/routes/$p | jq -e '.data.routes | length == 1'"
done

# BMP station: Netom sends Initiation, Peer Up, the table dump, then live updates
run_bgpipe -- bmp "127.0.0.1:$BMPPORT"

# initial dump: all 3 prefixes, attributed to our BGP session
for p in 192.0.2.0/24 198.51.100.0/24 10.99.0.0/16; do
	wait_json 20 ".[3]==\"UPDATE\" and .[5].PEER_AS==\"65010\" and (.[4].reach // [] | index(\"$p\"))"
done
wait_json 20 '.[4].reach==["192.0.2.0/24"] and .[4].attrs.ASPATH.value==[65010,65001] and .[5].ROUTER=="netom-test"'

# live: a withdrawal on the BGP session must come out of the BMP stream
echo '{"unreach": ["198.51.100.0/24"]}' >&9
wait_json 20 '.[3]=="UPDATE" and .[5].PEER_AS=="65010" and (.[4].unreach // [] | index("198.51.100.0/24"))'
wait_cmd 20 "curl -sf $API/ribs/ipv4unicast/routes/10.99.0.0/16 | jq -e '.data.routes | length == 1'"

# both bgpipes must still be up, and Netom must not have complained
kill -0 "$BGPIPE_PID" 2>/dev/null || fail "BMP reader died"
if jq -e 'select(.[3]=="NOTIFY")' <"$WORK/feeder.json" >/dev/null 2>&1; then
	fail "Netom sent a NOTIFICATION"
fi
