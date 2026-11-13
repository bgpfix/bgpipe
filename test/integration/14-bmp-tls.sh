#!/usr/bin/env bash
# bmp over TLS: an exporter with --cert/--key, a station with --tls; no docker needed
. "$(dirname "$0")/lib.sh"
command -v openssl >/dev/null 2>&1 || skip "openssl not available"

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=localhost \
	-keyout "$WORK/key.pem" -out "$WORK/cert.pem" 2>/dev/null || fail "openssl req"
freeport

# exporter: a FIFO keeps the BMP session up
mkfifo "$WORK/in.json"
"$BGPIPE" --log info -- read "$WORK/in.json" \
	-- bmp --write --listen --peer 127.0.0.1 --cert "$WORK/cert.pem" --key "$WORK/key.pem" \
	"127.0.0.1:$LPORT" 2>"$WORK/export.log" &
PIDS="$PIDS $!"
exec 9>"$WORK/in.json"
wait_cmd 10 "grep -q 'listening on' '$WORK/export.log'"

run_bgpipe -- bmp --tls --insecure "127.0.0.1:$LPORT"
wait_cmd 10 "grep -q 'connected 127.0.0.1' '$WORK/export.log'"

cat "$TESTDATA/updates.json" >&9
wait_prefix 10 "192.0.2.0/24"
