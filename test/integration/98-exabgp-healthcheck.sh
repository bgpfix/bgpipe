#!/usr/bin/env bash
# bgpipe in place of ExaBGP: runs ExaBGP's own healthcheck tool, unmodified
# (exec --format exa), and announces its service IP to GoBGP while the check passes
. "$(dirname "$0")/lib.sh"
need_docker

docker build -q -t bgpipe-test-exabgp -f "$TESTDATA/exabgp.Dockerfile" "$TESTDATA" >/dev/null

run_daemon gobgp 1790 \
	-v "$TESTDATA/gobgp-passive.conf:/config/gobgp.toml:ro" \
	jauderho/gobgp:latest
wait_cmd 20 "docker exec $DAEMON gobgp global"

# NB: healthcheck waits for "done" after each command, as ExaBGP answers
HC="$RUN_ID-hc"
run_bgpipe -- speaker --active --asn 65000 --id 10.0.0.2 \
	-- exec --format exa --read --wait ESTABLISHED -A \
	docker run --rm -i --label "$RUN_ID" --name "$HC" bgpipe-test-exabgp \
	exabgp healthcheck --no-syslog --no-ip-setup --ip 10.98.0.1/32 --next-hop 192.0.2.1 \
	--cmd "test ! -e /tmp/down" --interval 1 --rise 1 --fall 1 --withdraw-on-down \
	--community 65000:98 -- \
	-- connect "127.0.0.1:$PORT"

rib="docker exec $DAEMON gobgp global rib"
wait_cmd 30 "$rib | grep -F 10.98.0.1/32 | grep -F 65000:98"

# the check fails: the route goes away; and comes back
docker exec "$HC" touch /tmp/down
wait_cmd 20 "! $rib | grep -F 10.98.0.1/32"
docker exec "$HC" rm /tmp/down
wait_cmd 20 "$rib | grep -F 10.98.0.1/32"
