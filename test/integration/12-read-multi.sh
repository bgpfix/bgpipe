#!/usr/bin/env bash
# read -A: several files, one stream, in order; a missing path fails before anything is read
. "$(dirname "$0")/lib.sh"

gzip -c "$TESTDATA/updates.json" > "$WORK/second.json.gz"

run_bgpipe -- read -A "$TESTDATA/updates.json" "$WORK/second.json.gz" "$TESTDATA/updates.json" -- stdout

wait_prefix 10 "192.0.2.0/24"
sleep 1
n=$(jq -c 'select(.[3]=="UPDATE")' "$WORK/out.json" | wc -l | tr -d ' ')
[ "$n" = 9 ] || fail "expected 9 UPDATEs (3 files x 3), got $n"

# NB: the typo must be caught up front, not after the first file was read
if "$BGPIPE" -- read -A "$TESTDATA/updates.json" "$WORK/missing.json" -- stdout >"$WORK/bad.out" 2>"$WORK/bad.err"; then
	fail "missing path accepted"
fi
[ ! -s "$WORK/bad.out" ] || fail "read started despite a missing path"
