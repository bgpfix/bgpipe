#!/bin/bash
# aspa-live-stats.sh — run aspa-live with metrics exposed; print final counts.
#
# Usage:
#   aspa-live-stats.sh <rpki.json> [duration]   (default 60s)
set -euo pipefail

RPKI="${1:?rpki.json path required}"
DURATION="${2:-60s}"
PORT="${PORT:-17890}"
BGPIPE="${BGPIPE:-$(dirname "$0")/../bgpipe}"

INVALID_OUT="${INVALID_OUT:-/tmp/aspa-live-invalids.jsonl}"
STDERR_LOG="${STDERR_LOG:-/tmp/aspa-live.stderr}"
: > "$INVALID_OUT"
: > "$STDERR_LOG"

echo "# streaming ris-live for $DURATION, http on 127.0.0.1:$PORT" >&2

"$BGPIPE" --http 127.0.0.1:$PORT --http-open --rpki "$RPKI" \
	-- ris-live \
	-- aspa --role provider --peer-tag PEER_AS --invalid keep \
	-- stdout --if 'tag[aspa/status] == INVALID' \
	> "$INVALID_OUT" 2> "$STDERR_LOG" &
PID=$!

# wait for http to come up
for _ in $(seq 1 30); do
	if curl -sSf "http://127.0.0.1:$PORT/metrics" >/dev/null 2>&1; then break; fi
	sleep 0.2
done

# run for DURATION
sleep "$DURATION" || true

# fetch metrics BEFORE killing
echo "--- aspa metrics ---" >&2
curl -s "http://127.0.0.1:$PORT/metrics" | grep -E 'bgpipe_(aspa|rpki)_' | grep -v '^#' | sort >&2 || true

kill -INT "$PID" 2>/dev/null || true
wait "$PID" 2>/dev/null || true
echo "--- invalid messages written ---" >&2
wc -l "$INVALID_OUT" >&2
