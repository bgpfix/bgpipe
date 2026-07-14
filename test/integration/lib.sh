# shared helpers for bgpipe integration tests; sourced by NN-*.sh, see run.sh
# NB: keep portable to macOS bash 3.2 (no assoc arrays, no GNU timeout/stdbuf)
set -eu
set -o pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
TESTDATA="$DIR/testdata"
RUN_ID="bgpipe-test-$$"
# NB: keep workdir under the repo (ie. $HOME): docker VMs on macOS (colima,
# Docker Desktop) don't share /tmp, so bind mounts from /tmp appear empty
WORK="$DIR/.cache/work.$$"
mkdir -p "$WORK"
PIDS=""

msg() { echo "[$(basename "$0")] $*" >&2; }
fail() { msg "FAIL: $*"; exit 1; }
skip() { msg "SKIP: $*"; exit 77; }

# bgpipe binary: use $BGPIPE_BIN, or build once into .cache/
if [ -n "${BGPIPE_BIN:-}" ]; then
	BGPIPE="$BGPIPE_BIN"
else
	BGPIPE="$DIR/.cache/bgpipe"
	if [ ! -x "$BGPIPE" ]; then
		msg "building bgpipe..."
		mkdir -p "$DIR/.cache"
		(cd "$DIR/../.." && go build -o "$BGPIPE" .)
	fi
fi

# on exit: dump state if failed, kill bgpipe, remove labeled containers
cleanup() {
	rc=$?
	if [ $rc -ne 0 ] && [ $rc -ne 77 ]; then
		for c in $(docker ps -aq -f "label=$RUN_ID" 2>/dev/null || true); do
			msg "--- docker logs $c (tail) ---"
			docker logs "$c" 2>&1 | tail -30 >&2 || true
		done
		if [ -s "$WORK/bgpipe.log" ]; then
			msg "--- bgpipe log (tail) ---"
			tail -20 "$WORK/bgpipe.log" >&2
		fi
		if [ -s "$WORK/out.json" ]; then
			msg "--- bgpipe output ---"
			cat "$WORK/out.json" >&2
		fi
	fi
	if [ -n "$PIDS" ]; then kill $PIDS 2>/dev/null || true; fi
	# NB: || true keeps cleanup going when docker is missing or its daemon is down
	docker ps -aq -f "label=$RUN_ID" 2>/dev/null | while read -r c; do
		docker rm -f "$c" >/dev/null 2>&1 || true
	done || true
	if [ $rc -ne 0 ] && [ -n "${KEEP_WORK:-}" ]; then msg "work dir kept: $WORK"; else rm -rf "$WORK"; fi
	exit $rc
}
trap cleanup EXIT

# skip (or fail under CI) when docker is not usable
need_docker() {
	if ! docker info >/dev/null 2>&1; then
		if [ -n "${CI:-}" ]; then fail "docker not available in CI"; fi
		skip "docker not available"
	fi
}

# run_daemon <name> <container-port|-> <docker-run-args...>
# starts a labeled container; sets $DAEMON (id) and $PORT (mapped host port)
run_daemon() {
	local name="$1" cport="$2"
	shift 2
	local pub=""
	if [ "$cport" != "-" ]; then pub="-p 127.0.0.1:0:$cport"; fi
	DAEMON=$(docker run -d --label "$RUN_ID" $pub "$@") || fail "docker run $name"
	PORT=""
	if [ "$cport" != "-" ]; then
		PORT=$(docker port "$DAEMON" "$cport" | head -1 | sed 's/.*://')
		[ -n "$PORT" ] || fail "no mapped port for $name"
	fi
	msg "$name: container ${DAEMON%"${DAEMON#????????????}"} port ${PORT:-none}"
}

# wait_tcp <host> <port> [timeout-seconds]
wait_tcp() {
	local h="$1" p="$2" t="${3:-30}" i=0
	while ! (exec 3<>"/dev/tcp/$h/$p") 2>/dev/null; do
		i=$((i + 1))
		[ $i -ge $((t * 5)) ] && fail "timeout waiting for $h:$p"
		sleep 0.2
	done
	msg "$h:$p is up"
}

# run_bgpipe <args...> - background bgpipe, JSON output to $WORK/out.json
run_bgpipe() {
	msg "bgpipe $*"
	"$BGPIPE" --log "${BGPIPE_LOG:-warn}" "$@" >"$WORK/out.json" 2>"$WORK/bgpipe.log" &
	BGPIPE_PID=$!
	PIDS="$PIDS $BGPIPE_PID"
}

# wait_json <timeout-seconds> <jq-expr> - wait for an output line matching expr
wait_json() {
	local t="$1" expr="$2" i=0
	while ! jq -e "select($expr)" <"$WORK/out.json" >/dev/null 2>&1; do
		i=$((i + 1))
		[ $i -ge $((t * 5)) ] && fail "timeout waiting for JSON: $expr"
		sleep 0.2
	done
	msg "matched: $expr"
}

# wait_prefix <timeout-seconds> <prefix> - wait for an UPDATE carrying prefix
wait_prefix() {
	wait_json "$1" ".[3]==\"UPDATE\" and (tostring|contains(\"$2\"))"
}

# wait_exec <container> <timeout-seconds> <cmd...> - wait until cmd succeeds in container
wait_exec() {
	local c="$1" t="$2" i=0
	shift 2
	while ! docker exec "$c" "$@" >/dev/null 2>&1; do
		i=$((i + 1))
		[ $i -ge $((t * 5)) ] && fail "timeout waiting for: docker exec $c $*"
		sleep 0.2
	done
	msg "daemon ready: $*"
}

# wait_cmd <timeout-seconds> <shell-command> - wait until the command succeeds
# (runs on the host, so pipes work; use for shell-less containers)
wait_cmd() {
	local t="$1" cmd="$2" i=0
	while ! eval "$cmd" >/dev/null 2>&1; do
		i=$((i + 1))
		[ $i -ge $((t * 5)) ] && fail "timeout waiting for: $cmd"
		sleep 0.2
	done
	msg "ready: $cmd"
}

# host_ip - sets $HOSTIP to the address containers can reach the host at
host_ip() {
	HOSTIP=$(docker run --rm --add-host=hostgw:host-gateway alpine:3.22 \
		sh -c 'getent hosts hostgw' | awk '{print $1}' | tail -1)
	[ -n "$HOSTIP" ] || fail "cannot determine host gateway IP"
	msg "host is $HOSTIP from containers"
}

# freeport - sets $LPORT to a free TCP port on the host
freeport() {
	LPORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null) ||
		LPORT=$(((RANDOM % 20000) + 20000))
	msg "using local port $LPORT"
}

# render <testdata-template> <workfile> - substitute @HOSTIP@ and @PORT@
render() {
	sed -e "s/@HOSTIP@/$HOSTIP/g" -e "s/@PORT@/$LPORT/g" "$TESTDATA/$1" >"$WORK/$2"
}
