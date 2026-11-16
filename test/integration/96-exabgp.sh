#!/usr/bin/env bash
# ExaBGP interop: bgpipe rewrites ExaBGP API commands (--format exa), a real ExaBGP
# runs them and dials in to bgpipe, which must print the very same routes back
. "$(dirname "$0")/lib.sh"
need_docker

docker build -q -t bgpipe-test-exabgp -f "$TESTDATA/exabgp.Dockerfile" "$TESTDATA" >/dev/null

freeport
host_ip
render exabgp.conf.in exabgp.conf
cp "$TESTDATA/exabgp-feed.sh" "$WORK/feed.sh"

# what bgpipe makes of the commands must be good enough for ExaBGP
"$BGPIPE" --log warn -- read "$TESTDATA/exabgp.exa" -- stdout --format exa >"$WORK/routes.exa"
[ "$(wc -l <"$WORK/routes.exa")" -eq 10 ] || fail "expected 10 routes in routes.exa"

run_bgpipe --stdout -- listen "0.0.0.0:$LPORT" -- speaker --asn 65000 --id 10.0.0.3

run_daemon exabgp - \
	--add-host=hostgw:host-gateway \
	-e "exabgp_tcp_port=$LPORT" \
	-v "$WORK:/etc/exabgp:ro" \
	bgpipe-test-exabgp

wait_prefix 30 "10.96.5.0/24"

# NB: ExaBGP prepends its ASN and adds ORIGIN; LOCAL_PREF does not cross eBGP
"$BGPIPE" --log warn -- read "$WORK/out.json" -- stdout --format exa |
	sed -e 's/ origin IGP//' -e 's/ as-path \[ 65096 \]//' -e 's/as-path \[ 65096 /as-path [ /' >"$WORK/got.exa"
sed -e 's/ origin IGP//' -e 's/ local-preference 200//' "$WORK/routes.exa" >"$WORK/want.exa"
diff "$WORK/want.exa" "$WORK/got.exa" >&2 || fail "routes differ after the ExaBGP round trip"
msg "all routes came back intact"
