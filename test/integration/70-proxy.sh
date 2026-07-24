#!/usr/bin/env bash
# transparent proxy: GoBGP -> bgpipe (listen -- connect, no speaker) -> FRR;
# the BGP session negotiates end-to-end THROUGH bgpipe, routes flow both ways
. "$(dirname "$0")/lib.sh"
need_docker

# FRR side: passive, announces two networks (see frr.conf)
run_daemon frr 179 \
	--cap-add NET_ADMIN --cap-add SYS_ADMIN \
	-v "$TESTDATA/frr.conf:/etc/frr/frr.conf" \
	-v "$TESTDATA/frr-daemons:/etc/frr/daemons" \
	-v "$TESTDATA/frr-vtysh.conf:/etc/frr/vtysh.conf" \
	quay.io/frrouting/frr:10.2.1
FRR="$DAEMON" FRRPORT="$PORT"
wait_exec "$FRR" 60 sh -c "netstat -tln | grep -q :179"
wait_tcp 127.0.0.1 "$FRRPORT" 30

# bgpipe in the middle: pure pass-through, no BGP stack of its own
freeport
host_ip
run_bgpipe --stdout -- listen "0.0.0.0:$LPORT" -- connect --wait listen "127.0.0.1:$FRRPORT"

# GoBGP side: dials into bgpipe
render gobgp-active.conf.in gobgp.toml
run_daemon gobgp - \
	--add-host=hostgw:host-gateway \
	-v "$WORK/gobgp.toml:/config/gobgp.toml:ro" \
	jauderho/gobgp:latest
GOBGP="$DAEMON"

# both OPENs pass through bgpipe (one per direction)
wait_json 30 '.[3]=="OPEN" and .[0]=="R"'
wait_json 30 '.[3]=="OPEN" and .[0]=="L"'

# FRR's networks must arrive in GoBGP's RIB through the proxy
# NB: wait_cmd, not wait_exec: the gobgp image has no shell for pipes
wait_cmd 30 "docker exec $GOBGP gobgp global rib | grep -q 10.50.0.0/24"
wait_cmd 10 "docker exec $GOBGP gobgp global rib | grep -q 10.51.0.0/24"

# and a GoBGP announcement must land in FRR's RIB
docker exec "$GOBGP" gobgp global rib add 10.7.0.0/24 origin igp
wait_cmd 30 "docker exec $FRR vtysh -c 'show bgp ipv4 unicast' | grep -q 10.7.0.0/24"
