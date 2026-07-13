#!/usr/bin/env bash
# bgpipe dials FRR (listen range = dynamic neighbors), receives its networks
. "$(dirname "$0")/lib.sh"
need_docker

# NB: /etc/frr must be writable for docker-start; frr needs extra caps
run_daemon frr 179 \
	--cap-add NET_ADMIN --cap-add SYS_ADMIN \
	-v "$TESTDATA/frr.conf:/etc/frr/frr.conf" \
	-v "$TESTDATA/frr-daemons:/etc/frr/daemons" \
	-v "$TESTDATA/frr-vtysh.conf:/etc/frr/vtysh.conf" \
	quay.io/frrouting/frr:10.2.1
# NB: the mapped host port is not a reliable readiness probe (docker/colima
# forwarders accept or refuse on their own); wait for bgpd's listener itself
wait_exec 60 sh -c "netstat -tln | grep -q :179"
wait_tcp 127.0.0.1 "$PORT" 30

run_bgpipe --stdout -- speaker --active --asn 65000 --id 10.0.0.5 -- connect "127.0.0.1:$PORT"

wait_json 30 '.[3]=="OPEN" and .[0]=="L"'
wait_prefix 20 "10.50.0.0/24"
wait_prefix 20 "10.51.0.0/24"
