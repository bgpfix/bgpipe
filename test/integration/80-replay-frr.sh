#!/usr/bin/env bash
# real-world replay: 500 real RouteViews UPDATEs (full attribute zoo: MP-BGP
# IPv6, communities, aggregator, withdrawals) are re-marshaled by bgpipe and
# announced to FRR; every message must be accepted with no NOTIFICATION
. "$(dirname "$0")/lib.sh"
need_docker

run_daemon frr 179 \
	--cap-add NET_ADMIN --cap-add SYS_ADMIN \
	-v "$TESTDATA/frr-replay.conf:/etc/frr/frr.conf" \
	-v "$TESTDATA/frr-daemons:/etc/frr/daemons" \
	-v "$TESTDATA/frr-vtysh.conf:/etc/frr/vtysh.conf" \
	quay.io/frrouting/frr:10.2.1
wait_exec "$DAEMON" 60 sh -c "netstat -tln | grep -q :179"
wait_tcp 127.0.0.1 "$PORT" 30

# how many UPDATEs will be sent?
want=$("$BGPIPE" --log disabled -- read "$TESTDATA/sample.mrt" -- stdout |
	jq -c 'select(.[3]=="UPDATE")' | wc -l | tr -d ' ')
msg "replaying $want UPDATEs"

# read from a FIFO so the session stays up after the replay ends; eBGP
# collector-style session (remote 65001, no first-AS check, see frr-replay.conf);
# the update stage forces every message through bgpipe's marshaling path
mkfifo "$WORK/replay.mrt"
run_bgpipe -- read "$WORK/replay.mrt" \
	-- update --add-com "65000:1" \
	-- speaker --active --asn 65001 --id 10.0.0.8 \
	-- connect "127.0.0.1:$PORT"
exec 9>"$WORK/replay.mrt"
cat "$TESTDATA/sample.mrt" >&9

# wait until FRR has received everything, then check the session survived
i=0
while :; do
	i=$((i + 1))
	[ $i -gt 150 ] && fail "FRR did not receive $want UPDATEs in time"
	stats=$(docker exec "$DAEMON" vtysh -c "show bgp neighbors json" 2>/dev/null |
		jq -c '[.[].bgpState, (.[].messageStats | .updatesRecv, .notificationsSent)]')
	case "$stats" in
	"[\"Established\",$want,0]") break ;;
	esac
	sleep 0.2
done
msg "FRR accepted all $want UPDATEs, session still established: $stats"

# sanity: real routes made it into the RIB
docker exec "$DAEMON" vtysh -c "show bgp ipv4 unicast json" |
	jq -e '.routes | length > 100' >/dev/null || fail "too few IPv4 routes in FRR"
