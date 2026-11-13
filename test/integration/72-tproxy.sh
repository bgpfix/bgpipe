#!/usr/bin/env bash
# fully transparent proxy: GoBGP (L) <-> P running bgpipe with TPROXY <-> FRR (R);
# L and R peer with each other's real address, neither knows about P or bgpipe;
# three netns: L and R are daemons in docker (--network none) wired to P by veths
. "$(dirname "$0")/lib.sh"

[ "$(uname -s)" = Linux ] || skip "TPROXY needs Linux"
sudo -n true 2>/dev/null || skip "passwordless sudo not available"
for c in ip iptables nsenter ss; do
	command -v $c >/dev/null 2>&1 || skip "$c not found"
done
need_docker

# addresses: L 10.179.1.2 -- P 10.179.1.1 | 10.179.2.1 -- R 10.179.2.2
NS="bgpt-$$"
LADDR=10.179.1.2 RADDR=10.179.2.2

# P-side shortcuts
pns() { sudo -n ip netns exec "$NS" "$@"; }
cns() { local p="$1"; shift; sudo -n nsenter -t "$p" -n "$@"; }

# on exit: dump P state if failed, stop bgpipe, delete P (takes its rules, veths and
# routes along: nothing is ever configured in the host's main namespace), then lib.sh
tproxy_cleanup() {
	rc=$?
	if [ $rc -ne 0 ] && [ $rc -ne 77 ]; then
		msg "--- P: iptables -t mangle -S, ip rule, ip route ---"
		pns iptables -t mangle -S >&2 2>&1 || true
		pns ip rule >&2 2>&1 || true
		pns ip route show table all >&2 2>&1 || true
		msg "--- P: ss -tnp ---"
		pns ss -tnp >&2 2>&1 || true
	fi
	if [ -s "$WORK/bgpipe.pid" ]; then sudo -n kill "$(cat "$WORK/bgpipe.pid")" 2>/dev/null || true; fi
	sudo -n ip netns del "$NS" 2>/dev/null || true
	(exit $rc) && :
	cleanup
}
trap tproxy_cleanup EXIT

# holders own the netns of L and R, so we can wire them before the daemons start
run_daemon holder-l - --network none alpine:3.22 sleep 3600
HL="$DAEMON"
run_daemon holder-r - --network none alpine:3.22 sleep 3600
HR="$DAEMON"
LPID=$(docker inspect -f '{{.State.Pid}}' "$HL")
RPID=$(docker inspect -f '{{.State.Pid}}' "$HR")

# veths: created straight in the target namespaces, nothing touches the main one
sudo -n ip netns add "$NS"
sudo -n ip link add eth0 netns "$LPID" type veth peer name pl netns "$NS"
sudo -n ip link add eth0 netns "$RPID" type veth peer name pr netns "$NS"
cns "$LPID" ip addr add $LADDR/24 dev eth0
cns "$LPID" ip link set eth0 up
cns "$LPID" ip link set lo up
cns "$LPID" ip route add default via 10.179.1.1
cns "$RPID" ip addr add $RADDR/24 dev eth0
cns "$RPID" ip link set eth0 up
cns "$RPID" ip link set lo up
cns "$RPID" ip route add default via 10.179.2.1
pns ip addr add 10.179.1.1/24 dev pl
pns ip addr add 10.179.2.1/24 dev pr
pns ip link set pl up
pns ip link set pr up
pns ip link set lo up

# P is a plain router; rp_filter is relaxed for the fwmark-routed (TPROXY) packets
pns sysctl -qw net.ipv4.ip_forward=1
pns sysctl -qw net.ipv4.conf.all.rp_filter=0
pns sysctl -qw net.ipv4.conf.default.rp_filter=0
pns sysctl -qw net.ipv4.conf.pl.rp_filter=0
pns sysctl -qw net.ipv4.conf.pr.rp_filter=0

# TPROXY: BGP packets of an established proxied flow are delivered locally (DIVERT),
# new connections to port 179 (either direction) are handed to bgpipe's socket
# NB: no change of the packet's addresses, only of the socket it is delivered to
pns iptables -t mangle -N DIVERT
pns iptables -t mangle -A DIVERT -j MARK --set-mark 1
pns iptables -t mangle -A DIVERT -j ACCEPT
pns iptables -t mangle -A PREROUTING -p tcp -m socket -j DIVERT
pns iptables -t mangle -A PREROUTING -p tcp --dport 179 -j TPROXY --on-port 179 --tproxy-mark 1/1
# marked packets use a local route, so they reach bgpipe although dst is not P's address
pns ip rule add fwmark 1 lookup 100
pns ip route add local 0.0.0.0/0 dev lo table 100
# counts what P forwards as a router (zero while everything is diverted)
pns iptables -A FORWARD -p tcp --dport 179 -j ACCEPT
pns iptables -A FORWARD -p tcp --sport 179 -j ACCEPT

# bgpipe on P (needs CAP_NET_ADMIN for IP_TRANSPARENT); the pid is for root-side kill
msg "bgpipe (in $NS) --stdout -- listen --transparent :179 -- connect --transparent --wait listen 0.0.0.0"
sudo -n ip netns exec "$NS" sh -c 'echo $$ >"$1"; shift; exec "$@"' sh "$WORK/bgpipe.pid" \
	"$BGPIPE" --log "${BGPIPE_LOG:-warn}" --stdout \
	-- listen --transparent :179 -- connect --transparent --wait listen 0.0.0.0 \
	>"$WORK/out.json" 2>"$WORK/bgpipe.log" &
PIDS="$PIDS $!"
wait_cmd 30 "sudo -n ip netns exec $NS ss -tln | grep -q ':179 '"

# routers start only now; each is configured with the other's real address
docker create --label "$RUN_ID" --network "container:$HR" \
	--cap-add NET_ADMIN --cap-add SYS_ADMIN \
	-v "$TESTDATA/frr-tproxy.conf:/etc/frr/frr.conf" \
	-v "$TESTDATA/frr-daemons:/etc/frr/daemons" \
	-v "$TESTDATA/frr-vtysh.conf:/etc/frr/vtysh.conf" \
	quay.io/frrouting/frr:10.2.1 >"$WORK/frr.id" || fail "docker create frr"
FRR=$(cat "$WORK/frr.id")
docker create --label "$RUN_ID" --network "container:$HL" \
	-v "$TESTDATA/gobgp-tproxy.toml:/config/gobgp.toml:ro" \
	jauderho/gobgp:latest >"$WORK/gobgp.id" || fail "docker create gobgp"
GOBGP=$(cat "$WORK/gobgp.id")
# NB: R first and ready: bgpipe serves one connection only, and FRR rejects incoming
# connections for ~1s after start ("configuration being currently read in"); if R
# dropped the proxied one, the pipe would end and L could never retry
docker start "$FRR" >/dev/null || fail "docker start frr"
wait_cmd 60 "docker exec $FRR vtysh -c 'show bgp neighbors $LADDR' | grep -q 'BGP state = Active'"
docker start "$GOBGP" >/dev/null || fail "docker start gobgp"

# the session goes through bgpipe: one OPEN per direction
wait_json 60 '.[3]=="OPEN" and .[0]=="R"'
wait_json 30 '.[3]=="OPEN" and .[0]=="L"'
wait_cmd 30 "docker exec $FRR vtysh -c 'show bgp neighbors $LADDR' | grep -q 'BGP state = Established'"

# FRR's networks must arrive in GoBGP's RIB, and vice versa
# NB: wait_cmd, not wait_exec: the gobgp image has no shell for pipes
wait_cmd 30 "docker exec $GOBGP gobgp global rib | grep -q 10.50.0.0/24"
wait_cmd 10 "docker exec $GOBGP gobgp global rib | grep -q 10.51.0.0/24"
docker exec "$GOBGP" gobgp global rib add 10.7.0.0/24 origin igp
wait_cmd 30 "docker exec $FRR vtysh -c 'show bgp ipv4 unicast' | grep -q 10.7.0.0/24"

# each router sees the other's real address as its TCP peer, never P's
fr=$(docker exec "$FRR" vtysh -c "show bgp neighbors $LADDR")
echo "$fr" | grep -E 'Local host|Foreign host' >&2
echo "$fr" | grep -q "Foreign host: $LADDR," || fail "FRR: remote end is not $LADDR"
echo "$fr" | grep -q "Local host: $RADDR," || fail "FRR: local end is not $RADDR"
ssl=$(cns "$LPID" ss -tn state established '( dport = :179 )')
ssr=$(cns "$RPID" ss -tn state established '( sport = :179 )')
msg "L: $(echo "$ssl" | tail -1)"
msg "R: $(echo "$ssr" | tail -1)"
echo "$ssl" | grep -q "$LADDR:[0-9]* *$RADDR:179" || fail "L: no connection to $RADDR:179"
echo "$ssr" | grep -q "$RADDR:179 *$LADDR:[0-9]*" || fail "R: no connection from $LADDR"
echo "$ssl $ssr" | grep -q '10\.179\.[12]\.1[: ]' && fail "a router talks to P's address"

# P really terminates both legs (spoofed addresses), and forwarded nothing
pt=$(pns ss -tn state established)
msg "P: $(echo "$pt" | grep -c ':179') BGP sockets"
[ "$(echo "$pt" | grep -c ':179')" -eq 2 ] || fail "P: expected two bgpipe sockets"
fw=$(pns iptables -t filter -nvx -L FORWARD | awk '$3=="ACCEPT"{n+=$1} END{print n+0}')
[ "$fw" -eq 0 ] || fail "P forwarded $fw BGP packets while diverting"

# paper claim: without TPROXY and bgpipe, P is a plain router and the session returns
msg "removing TPROXY and stopping bgpipe"
pns iptables -t mangle -F PREROUTING
pns iptables -t mangle -F DIVERT
pns iptables -t mangle -X DIVERT
pns ip rule del fwmark 1 lookup 100
pns ip route flush table 100
sudo -n kill "$(cat "$WORK/bgpipe.pid")"
wait_cmd 30 "! docker exec $FRR vtysh -c 'show bgp neighbors $LADDR' | grep -q 'BGP state = Established'"
wait_cmd 30 "docker exec $FRR vtysh -c 'show bgp neighbors $LADDR' | grep -q 'BGP state = Established'"
docker exec "$GOBGP" gobgp global rib add 10.8.0.0/24 origin igp
wait_cmd 30 "docker exec $FRR vtysh -c 'show bgp ipv4 unicast' | grep -q 10.8.0.0/24"
wait_cmd 30 "docker exec $GOBGP gobgp global rib | grep -q 10.50.0.0/24"
[ "$(pns iptables -t mangle -S | grep -c TPROXY)" -eq 0 ] || fail "TPROXY rule still present"
# NB: bgpipe's orphaned legs may linger in FIN-WAIT-1 for a while, they are not sessions
[ "$(pns ss -tan state established | grep -c ':179')" -eq 0 ] || fail "P still terminates BGP"
fw=$(pns iptables -t filter -nvx -L FORWARD | awk '$3=="ACCEPT"{n+=$1} END{print n+0}')
msg "P forwarded $fw BGP packets directly"
[ "$fw" -gt 0 ] || fail "session did not go through P as a router"
