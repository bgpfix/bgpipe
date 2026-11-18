#!/usr/bin/env bash
# combine: UPDATEs split into one per prefix (like rv-live) are put back together,
# with every prefix change kept, in order
. "$(dirname "$0")/lib.sh"

# flat FILE: per-prefix changes in FILE, in order for each prefix
flat() {
	jq -r -f "$TESTDATA/flat.jq" "$1" | awk '{print NR, $0}' | sort -s -k2,4 -k1,1n | cut -d' ' -f2-
}

"$BGPIPE" --log warn -- read "$TESTDATA/sample.mrt" -- write "$WORK/orig.json" || fail "read failed"
jq -c -f "$TESTDATA/split.jq" "$WORK/orig.json" > "$WORK/split.json"
"$BGPIPE" --log warn -- read "$WORK/split.json" -- combine -- write "$WORK/comb.json" || fail "combine failed"

orig=$(grep -c UPDATE "$WORK/orig.json")
split=$(grep -c UPDATE "$WORK/split.json")
comb=$(grep -c UPDATE "$WORK/comb.json")
msg "UPDATEs: $orig original, $split split, $comb combined"
[ "$comb" -le "$orig" ] || fail "combined into more UPDATEs than the original"

[ "$(flat "$WORK/orig.json")" = "$(flat "$WORK/comb.json")" ] || fail "prefix changes differ"
