#!/usr/bin/env bash
# smooth: the last state of each prefix is kept, all withdrawals too, fewer announcements
. "$(dirname "$0")/lib.sh"

"$BGPIPE" --log warn -- read "$TESTDATA/sample.mrt" -- write "$WORK/orig.json" || fail "read failed"
"$BGPIPE" --log warn -- read "$TESTDATA/sample.mrt" -- smooth -w 10s -- write "$WORK/smooth.json" || fail "smooth failed"

for x in orig smooth; do
	jq -r -f "$TESTDATA/events.jq" "$WORK/$x.json" > "$WORK/$x.ev"
	awk '{k=$2" "$3; v[k]=$4" "$5} END {for (k in v) print k, v[k]}' "$WORK/$x.ev" | sort > "$WORK/$x.final"
done

a0=$(grep -c ' A ' "$WORK/orig.ev"); a1=$(grep -c ' A ' "$WORK/smooth.ev")
w0=$(grep -c ' W ' "$WORK/orig.ev"); w1=$(grep -c ' W ' "$WORK/smooth.ev")
msg "announcements: $a0 -> $a1, withdrawals: $w0 -> $w1"
[ "$a1" -lt "$a0" ] || fail "no announcements held back"
[ "$w1" -eq "$w0" ] || fail "withdrawals lost"
cmp -s "$WORK/orig.final" "$WORK/smooth.final" || fail "final prefix states differ"
