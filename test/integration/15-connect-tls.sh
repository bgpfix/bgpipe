#!/usr/bin/env bash
# connect --tls: messages reach a TLS server; no docker needed
. "$(dirname "$0")/lib.sh"
command -v openssl >/dev/null 2>&1 || skip "openssl not available"

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=localhost \
	-keyout "$WORK/key.pem" -out "$WORK/cert.pem" 2>/dev/null || fail "openssl req"

freeport
sleep 60 | openssl s_server -accept "$LPORT" -key "$WORK/key.pem" -cert "$WORK/cert.pem" -quiet \
	>"$WORK/tls.out" 2>/dev/null &
PIDS="$PIDS $!"
wait_tcp 127.0.0.1 "$LPORT" 10

run_bgpipe -- read "$TESTDATA/updates.json" -- connect --tls --insecure "127.0.0.1:$LPORT"

# every BGP message starts with a 16-byte 0xff marker; updates.json has 3 UPDATEs
wait_cmd 10 "[ \$(od -An -v -tx1 '$WORK/tls.out' | tr -d ' \n' | grep -o ffffffffffffffffffffffffffffffff | wc -l) -ge 3 ]"
