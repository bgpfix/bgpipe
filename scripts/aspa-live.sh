#!/bin/bash
# aspa-live.sh — real-world ASPA validation against RIPE RIS Live.
#
# Usage:
#   aspa-live.sh <rpki.json> [duration]
#
# Streams UPDATEs from RIS Live through the aspa stage (monitor-mode so
# nothing is withdrawn) and writes each ASPA-INVALID result to stdout as
# a single JSON line. Prints counters to stderr on SIGINT / duration expiry.
#
# Why --role=provider: ris-live delivers raw BGP UPDATEs without the
# peer's OPEN, so auto-detection can't read the BGP Role capability.
# RIS peers typically announce their full Internet view to the collector,
# which from the collector's perspective is a "received from provider"
# relationship — downstream validation with valley-free up-ramp/down-ramp
# pattern (draft-ietf-sidrops-aspa-verification §5.5). Using "customer"
# would force strict upstream validation and flag every path that
# traverses a Tier-1 whose ASPA declares "no providers" ([0]).
# --peer-tag PEER_AS reads the peer ASN from ris-live tags, enabling
# the first-hop check.
set -euo pipefail

RPKI="${1:?rpki.json path required}"
DURATION="${2:-30s}"

BGPIPE="${BGPIPE:-$(dirname "$0")/../bgpipe}"
if [[ ! -x "$BGPIPE" ]]; then
	echo "bgpipe binary not found at $BGPIPE — set BGPIPE=/path/to/bgpipe" >&2
	exit 2
fi

echo "# streaming ris-live for $DURATION, rpki file $RPKI" >&2

STDERR_LOG="${STDERR_LOG:-/tmp/aspa-live.stderr}"
timeout --preserve-status --signal=INT "$DURATION" \
	"$BGPIPE" --rpki "$RPKI" \
	-- ris-live \
	-- aspa --role provider --peer-tag PEER_AS --invalid keep \
	-- stdout --if 'tag[aspa/status] == INVALID' \
	2> >(tee "$STDERR_LOG" >&2) || true

echo "# counters (from aspa stderr):" >&2
grep -E 'RPKI cache updated|ASPA:' "$STDERR_LOG" >&2 || true
