#!/usr/bin/env bash
# bgpipe aspa stage fed over RTRv2 by rtrtr serving the local rpki.json fixture
# NB: rtrtr (not StayRTR/Routinator): StayRTR has no ASPA support, and
# Routinator 0.15 parses SLURM aspaAssertions but does not serve them
. "$(dirname "$0")/lib.sh"
need_docker

run_daemon rtrtr 3323 \
	-v "$TESTDATA/rtrtr.conf:/rtrtr.conf:ro" \
	-v "$TESTDATA/rpki.json:/rpki.json:ro" \
	nlnetlabs/rtrtr:latest -c /rtrtr.conf
wait_tcp 127.0.0.1 "$PORT"

# multi-peer feed style: peer ASN from the PEER_AS tag, explicit --role
run_bgpipe --stdout --rpki "127.0.0.1:$PORT" \
	-- read "$TESTDATA/aspa-updates.json" \
	-- aspa --role customer --peer-tag PEER_AS

# path [65010, 65020]: 65020->{65010} attested; [65010, 65099]: 65099->{65030} only
wait_json 30 '.[5]["aspa/status"]=="VALID" and .[4].reach==["203.0.113.0/24"]'
wait_json 10 '.[5]["aspa/status"]=="INVALID" and .[5]["aspa/invalid-hop"]=="65099 65010"'
