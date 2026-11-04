#!/usr/bin/env bash
# BIRD with 2-byte ASNs only: bgpipe must parse the session and archive it as 2-byte MRT
. "$(dirname "$0")/lib.sh"
need_docker

docker build -q -t bgpipe-test-bird -f "$TESTDATA/bird.Dockerfile" "$TESTDATA" >/dev/null

freeport
host_ip
render bird-as2.conf.in bird.conf

run_bgpipe --stdout -- listen "0.0.0.0:$LPORT" \
	-- write -LR --format mrt "$WORK/as2.mrt" \
	-- speaker --asn 65000 --id 10.0.0.4

run_daemon bird - \
	--add-host=hostgw:host-gateway \
	-v "$WORK/bird.conf:/etc/bird.conf:ro" \
	bgpipe-test-bird

wait_json 30 '.[3]=="OPEN" and .[4].asn==65045 and (.[4].caps.AS4 | not)'
wait_prefix 20 "10.45.0.0/24"
wait_prefix 20 "10.46.0.0/24"
wait_json 10 '.[3]=="UPDATE" and .[4].attrs.ASPATH.value == [65045]'
wait_json 10 '.[3]=="UPDATE" and .[4].attrs.ASPATH.value == [65045,23456] and .[4].attrs.AS4PATH.value == [65045,4200000001]'
! grep -q PARSE "$WORK/bgpipe.log" || fail "parse errors: $(grep PARSE "$WORK/bgpipe.log")"

# the MRT archive must decode without guessing
kill "$BGPIPE_PID" 2>/dev/null; wait "$BGPIPE_PID" 2>/dev/null
"$BGPIPE" --as4=on -- read "$WORK/as2.mrt" -- stdout >"$WORK/back.json" 2>"$WORK/back.log" ||
	fail "readback: $(cat "$WORK/back.log")"
n=$(jq -c 'select(.[3]=="UPDATE" and .[4].attrs.ASPATH.value[0] == 65045)' "$WORK/back.json" | wc -l | tr -d ' ')
[ "$n" -ge 2 ] || fail "readback: expected >= 2 UPDATEs from AS65045, got $n"
