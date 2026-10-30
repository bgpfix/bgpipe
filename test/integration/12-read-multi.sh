#!/usr/bin/env bash
# read -A: several files, one stream, in order; a missing path fails before anything is read
. "$(dirname "$0")/lib.sh"

# NB: the second file differs, so that the order of files can be checked,
# and lacks the final newline, which must not glue its last line to the next file
sed 's|192\.0\.2\.0/24|203.0.113.0/24|' "$TESTDATA/updates.json" | head -c -1 | gzip -c > "$WORK/second.json.gz"

run_bgpipe -- read -A "$TESTDATA/updates.json" "$WORK/second.json.gz" "$TESTDATA/updates.json" -- stdout

wait_prefix 10 "203.0.113.0/24"
sleep 1
got=$(jq -r 'select(.[3]=="UPDATE") | .[4].reach[]' "$WORK/out.json" | paste -sd' ')
want="192.0.2.0/24 198.51.100.0/24 10.99.0.0/16 203.0.113.0/24 198.51.100.0/24 10.99.0.0/16 192.0.2.0/24 198.51.100.0/24 10.99.0.0/16"
[ "$got" = "$want" ] || fail "wrong UPDATEs or order: $got"

# NB: the typo must be caught up front, not after the first file was read
if "$BGPIPE" -- read -A "$TESTDATA/updates.json" "$WORK/missing.json" -- stdout >"$WORK/bad.out" 2>"$WORK/bad.err"; then
	fail "missing path accepted"
fi
[ ! -s "$WORK/bad.out" ] || fail "read started despite a missing path"
