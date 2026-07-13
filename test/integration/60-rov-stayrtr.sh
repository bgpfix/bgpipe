#!/usr/bin/env bash
# bgpipe rov stage fed over RTR by StayRTR serving a local rpki.json fixture
# NB: StayRTR has no ASPA support; ASPA-over-RTR needs another server (later phase)
. "$(dirname "$0")/lib.sh"
need_docker

run_daemon stayrtr 8282 \
	-v "$TESTDATA/rpki.json:/rpki.json:ro" \
	rpki/stayrtr:latest -bind :8282 -cache /rpki.json -checktime=false
wait_tcp 127.0.0.1 "$PORT"

# rov waits for the RTR cache before processing (see rov --no-wait)
run_bgpipe --stdout --rpki "127.0.0.1:$PORT" -- read "$TESTDATA/updates.json" -- rov

# 192.0.2.0/24 from AS65001 has a matching ROA; 198.51.100.0/24 from AS65099
# does not (and gets withdrawn by rov); 10.99.0.0/16 has no ROA at all
wait_json 30 '.[5]["rov/status"]=="VALID" and .[4].reach==["192.0.2.0/24"]'
wait_json 10 '.[5]["rov/status"]=="INVALID" and .[5]["rov/198.51.100.0/24"]=="INVALID"'
wait_json 10 '.[5]["rov/status"]=="NOT_FOUND" and .[4].reach==["10.99.0.0/16"]'
