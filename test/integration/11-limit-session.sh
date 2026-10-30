#!/usr/bin/env bash
# limit --session N passes exactly N prefixes; no docker needed
. "$(dirname "$0")/lib.sh"

run_bgpipe -- read "$TESTDATA/updates.json" -- limit --session 2 -- stdout

wait_prefix 10 "198.51.100.0/24"
sleep 1
n=$(jq -c 'select(.[3]=="UPDATE")' "$WORK/out.json" | wc -l | tr -d ' ')
[ "$n" = 2 ] || fail "expected 2 UPDATEs, got $n"
