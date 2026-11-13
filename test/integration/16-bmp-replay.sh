#!/usr/bin/env bash
# bmp exporter replaying files to a station, no docker needed: nothing may be lost
# at either end of the run, and ADD-PATH Path Identifiers must survive
. "$(dirname "$0")/lib.sh"
freeport

run_bgpipe -- bmp --listen "127.0.0.1:$LPORT"
wait_tcp 127.0.0.1 "$LPORT"

# the whole file: the first UPDATE flows as soon as the exporter starts, and the
# pipeline ends right after the last one
"$BGPIPE" --log warn -- read "$TESTDATA/sample.mrt" -- bmp --write "127.0.0.1:$LPORT"
want=$("$BGPIPE" --log warn -- read "$TESTDATA/sample.mrt" | jq -c 'select(.[3]=="UPDATE")' | wc -l | tr -d ' ')
wait_cmd 10 "[ \$(jq -c 'select(.[3]==\"UPDATE\")' '$WORK/out.json' | wc -l) -eq $want ]"
msg "all $want UPDATEs arrived"

# a BMP feed of an ADD-PATH peer (path IDs 1 and 2 for 10.0.0.0/24): the Peer Up
# we synthesize must tell the station about ADD-PATH
"$BGPIPE" --log warn -- read "$TESTDATA/addpath.bmp" -- bmp --write "127.0.0.1:$LPORT"
wait_json 10 '.[4].reach==["#2#10.0.0.0/24"] and .[5].PEER_AS=="65001"'
wait_json 10 '.[4].unreach==["#1#10.0.0.0/24"]'
if jq -n -e 'any(inputs; .[5].PEER_AS=="65001" and (.[4].reach // [] | index("0.0.0.0/0")))' <"$WORK/out.json" >/dev/null 2>&1; then
	fail "Path Identifiers decoded as prefixes"
fi

# a file from write --format bmp replays raw into a station (see 94 for Rotonda,
# which needs the Initiation and Peer Ups in it)
"$BGPIPE" --log warn -- read "$TESTDATA/sample.mrt" -- write --format bmp "$WORK/out.bmp"
n=$(jq -c 'select(.[3]=="UPDATE")' "$WORK/out.json" | wc -l | tr -d ' ')
cat "$WORK/out.bmp" >"/dev/tcp/127.0.0.1/$LPORT"
wait_cmd 10 "[ \$(jq -c 'select(.[3]==\"UPDATE\")' '$WORK/out.json' | wc -l) -eq $((n + want)) ]"
msg "raw replay of write --format bmp: all $want UPDATEs arrived"
