#!/usr/bin/env bash
# BIRD dials in, bgpipe streams an MRT file to it and exits once it is fully sent
# (speaker --stop read/STOP, see issue #40)
. "$(dirname "$0")/lib.sh"
need_docker

docker build -q -t bgpipe-test-bird -f "$TESTDATA/bird.Dockerfile" "$TESTDATA" >/dev/null

freeport
host_ip
render bird-feed.conf.in bird.conf

# the last IPv4 prefix announced in the file: if BIRD got it, it got everything
last=$("$BGPIPE" --log disabled -- read "$TESTDATA/sample.mrt" -- stdout |
	jq -r 'select(.[3]=="UPDATE") | .[4].reach // empty | .[]' | tail -1)
[ -n "$last" ] || fail "no IPv4 announcements in sample.mrt"
msg "last announced prefix: $last"

run_bgpipe -- speaker --active --asn 65000 --id 10.0.0.4 --stop read/STOP \
	-- read --wait ESTABLISHED "$TESTDATA/sample.mrt" \
	-- listen "0.0.0.0:$LPORT"

run_daemon bird - \
	--add-host=hostgw:host-gateway \
	-v "$WORK/bird.conf:/etc/bird.conf:ro" \
	bgpipe-test-bird

# bgpipe must exit on its own, cleanly
i=0
while kill -0 "$BGPIPE_PID" 2>/dev/null; do
	i=$((i + 1))
	[ $i -gt 150 ] && fail "bgpipe still running after the file was sent"
	sleep 0.2
done
rc=0
wait "$BGPIPE_PID" || rc=$?
[ $rc -eq 0 ] || fail "bgpipe exit code $rc"
msg "bgpipe exited cleanly"

docker logs "$DAEMON" 2>&1 | grep "added" | grep -qF "$last" || fail "BIRD did not receive $last"
msg "BIRD received the whole file"
