#!/usr/bin/env bash
# smoke test: JSON round-trip through the real binary; no docker needed
. "$(dirname "$0")/lib.sh"

run_bgpipe -- read "$TESTDATA/updates.json" -- stdout

wait_prefix 10 "192.0.2.0/24"
wait_prefix 10 "198.51.100.0/24"
wait_prefix 10 "10.99.0.0/16"

n=$(jq -c 'select(.[3]=="UPDATE")' "$WORK/out.json" | wc -l | tr -d ' ')
[ "$n" = 3 ] || fail "expected 3 UPDATEs, got $n"
