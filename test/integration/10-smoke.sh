#!/usr/bin/env bash
# smoke test: JSON round-trip through the real binary; no docker needed
. "$(dirname "$0")/lib.sh"

run_bgpipe -- read "$TESTDATA/updates.json" -- stdout

wait_prefix 10 "192.0.2.0/24"
wait_prefix 10 "198.51.100.0/24"
wait_prefix 10 "10.99.0.0/16"

n=$(jq -c 'select(.[3]=="UPDATE")' "$WORK/out.json" | wc -l | tr -d ' ')
[ "$n" = 3 ] || fail "expected 3 UPDATEs, got $n"

# head stops the pipeline cleanly: exit 0, nothing logged as an error
for i in 1 2 3; do
	"$BGPIPE" -- read "$TESTDATA/sample.mrt" -- head --count 2 -- stdout >"$WORK/head.json" 2>"$WORK/head.log" ||
		fail "head: exit code $?"
	[ "$(wc -l <"$WORK/head.json" | tr -d ' ')" = 2 ] || fail "head: expected 2 messages"
	! grep -q ERR "$WORK/head.log" || fail "head: $(cat "$WORK/head.log")"
done

# 2-byte ASNs in raw BGP are detected, and stay 2-byte in MRT output
"$BGPIPE" -- read --format raw "$TESTDATA/as2.raw" -- write --format mrt "$WORK/as2.mrt" 2>"$WORK/as2.log" ||
	fail "as2: $(cat "$WORK/as2.log")"
"$BGPIPE" --guess-asn=false -- read "$WORK/as2.mrt" -- stdout >"$WORK/as2.json" 2>>"$WORK/as2.log" ||
	fail "as2: $(cat "$WORK/as2.log")"
p=$(jq -c 'select(.[3]=="UPDATE") | .[4].attrs.ASPATH.value' "$WORK/as2.json" | head -1)
[ "$p" = "[65001,3356,174]" ] || fail "as2: wrong AS_PATH: $p"
