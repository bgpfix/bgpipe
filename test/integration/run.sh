#!/usr/bin/env bash
# bgpipe integration test runner: ./run.sh [pattern...]
# runs all NN-*.sh tests (or those matching any pattern), reports PASS/FAIL/SKIP
set -eu
cd "$(dirname "$0")"

command -v jq >/dev/null 2>&1 || { echo "error: jq is required" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 1; }

# build bgpipe once for all tests (override with BGPIPE_BIN)
if [ -z "${BGPIPE_BIN:-}" ]; then
	echo "building bgpipe..."
	mkdir -p .cache
	(cd ../.. && go build -o test/integration/.cache/bgpipe .)
	BGPIPE_BIN="$PWD/.cache/bgpipe"
	export BGPIPE_BIN
fi

pass=0 failn=0 skipn=0 failed=""
for t in [0-9][0-9]-*.sh; do
	if [ $# -gt 0 ]; then
		m=0
		for p in "$@"; do case "$t" in *"$p"*) m=1 ;; esac; done
		[ $m -eq 1 ] || continue
	fi
	echo "=== RUN  $t"
	rc=0
	bash "$t" || rc=$?
	if [ $rc -eq 0 ]; then
		pass=$((pass + 1)); echo "=== PASS $t"
	elif [ $rc -eq 77 ]; then
		skipn=$((skipn + 1)); echo "=== SKIP $t"
	else
		failn=$((failn + 1)); failed="$failed $t"; echo "=== FAIL $t"
	fi
done

echo "results: $pass passed, $failn failed, $skipn skipped"
if [ $failn -gt 0 ]; then echo "failed:$failed"; exit 1; fi
