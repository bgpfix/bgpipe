#!/usr/bin/env bash
# RTR resync: revoking a ROA at the validator must invalidate it in bgpipe's
# RPKI cache at the next RTR update (ROA revocations are routine ops)
. "$(dirname "$0")/lib.sh"
need_docker

# serve a writable copy of the fixture, so we can revoke a ROA later;
# stayrtr re-reads it every 1s and notifies clients of the new serial
cp "$TESTDATA/rpki.json" "$WORK/rpki.json"
# NB: mount the directory, not the file: the atomic-replace below (mv) makes
# a new inode, which a file bind-mount would pin to the old content on Linux
run_daemon stayrtr 8282 \
	-v "$WORK:/data" \
	rpki/stayrtr:latest -bind :8282 -cache /data/rpki.json -checktime=false -refresh 1
wait_tcp 127.0.0.1 "$PORT"

# read from a FIFO to keep the pipeline alive throughout the test
mkfifo "$WORK/in.json"
run_bgpipe --stdout --rpki "127.0.0.1:$PORT" --rpki-retry 2s \
	-- read "$WORK/in.json" -- rov
exec 9>"$WORK/in.json"

# 192.0.2.0/24 from AS65001 is VALID under the initial fixture
line=$(head -1 "$TESTDATA/updates.json")
echo "$line" >&9
wait_json 30 '.[1]==1 and .[5]["rov/status"]=="VALID"'

# revoke the ROA; stayrtr picks it up and pushes a Serial Notify
jq '.roas |= map(select(.prefix != "192.0.2.0/24"))' "$TESTDATA/rpki.json" >"$WORK/rpki.json.new"
mv "$WORK/rpki.json.new" "$WORK/rpki.json"
msg "ROA for 192.0.2.0/24 revoked at the validator"

# feed the same update until the revocation takes effect;
# a stale cache would keep returning VALID forever -> timeout
i=1
while :; do
	i=$((i + 1))
	[ $i -gt 30 ] && fail "revoked ROA still VALID (stale cache?)"
	echo "$line" | jq -c ".[1]=$i" >&9
	sleep 1
	if jq -e "select(.[1]==$i and .[5][\"rov/status\"]==\"NOT_FOUND\")" \
		<"$WORK/out.json" >/dev/null 2>&1; then
		break
	fi
done
msg "revoked ROA flushed after resync (seq $i)"
