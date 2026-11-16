#!/usr/bin/env bash
# bgpipe in place of ExaBGP: runs an ExaBGP-style API script (exec --format exa)
# and announces its routes to GoBGP
. "$(dirname "$0")/lib.sh"
need_docker

run_daemon gobgp 1790 \
	-v "$TESTDATA/gobgp-passive.conf:/config/gobgp.toml:ro" \
	jauderho/gobgp:latest
wait_cmd 20 "docker exec $DAEMON gobgp global"

run_bgpipe -- speaker --active --asn 65000 --id 10.0.0.2 \
	-- exec --format exa --read --wait ESTABLISHED "$TESTDATA/exabgp-script.sh" \
	-- connect "127.0.0.1:$PORT"

# NB: GoBGP takes a route only if it has all the mandatory attributes
wait_cmd 20 "docker exec $DAEMON gobgp global rib | grep -F 10.97.1.0/24"
docker exec "$DAEMON" gobgp global rib | grep -F 10.97.0.0/24 | grep -qF no-export ||
	fail "GoBGP did not get 10.97.0.0/24 with its communities"

# the script withdraws one of the routes
wait_cmd 20 "! docker exec $DAEMON gobgp global rib | grep -F 10.97.1.0/24"
docker exec "$DAEMON" gobgp global rib | grep -qF 10.97.0.0/24 || fail "GoBGP lost 10.97.0.0/24"
