#!/usr/bin/env bash
# bmp stage against itself, no docker needed: an exporter listens, a station
# connects with --retry; routes, peer tags and the router name must arrive
. "$(dirname "$0")/lib.sh"
freeport

# exporter: a FIFO keeps the BMP session up
mkfifo "$WORK/in.json"
"$BGPIPE" --log info -- read "$WORK/in.json" \
	-- bmp --write --listen --peer 127.0.0.1 --sys-name rtr1 "127.0.0.1:$LPORT" 2>"$WORK/export.log" &
PIDS="$PIDS $!"
exec 9>"$WORK/in.json"

run_bgpipe -- bmp --retry "127.0.0.1:$LPORT"
# NB: the exporter does not queue for stations that are yet to connect
wait_cmd 10 "grep -q 'connected 127.0.0.1' '$WORK/export.log'"

# peer and RIB view travel in message tags
jq -c '.[5]={"PEER_IP":"192.0.2.7","PEER_AS":"65007","BMP_RIB":"in-post"}' "$TESTDATA/updates.json" >&9
wait_prefix 10 "192.0.2.0/24"
wait_prefix 10 "10.99.0.0/16"
wait_json 10 '.[4].reach==["198.51.100.0/24"] and .[5].PEER_IP=="192.0.2.7" and .[5].PEER_AS=="65007" and .[5].PEER_ID=="192.0.2.7" and .[5].BMP_RIB=="in-post" and .[5].ROUTER=="rtr1"'

# only UPDATEs travel as Route Monitoring
echo '["R",9,"2026-07-13T00:00:09.000","KEEPALIVE",null,{}]' >&9
echo '{"unreach": ["192.0.2.0/24"]}' >&9
wait_json 10 '.[4].unreach==["192.0.2.0/24"]'
n=$(jq -c 'select(.[3]!="UPDATE")' "$WORK/out.json" | wc -l | tr -d ' ')
[ "$n" = 0 ] || fail "expected UPDATEs only, got $n other messages"
