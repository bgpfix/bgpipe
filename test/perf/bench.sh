#!/bin/bash
#
# bench.sh: compare bgpipe performance across releases, on the same input file.
#
# Usage: test/perf/bench.sh [-n RUNS] [-f FILE] [-o RESULTS.tsv] REF [REF...]
#
#   REF   a bgpipe git ref (eg. v0.25.0, v0.26.0, main): built with the bgpfix
#         version pinned in its go.mod, ie. the full release stack; or "work":
#         the current working trees of bgpipe and ../bgpfix (via a temp go.work)
#   -n    runs per task (default 3); the median is reported
#   -f    input MRT file (default: $BGPIPE_BENCH_FILE, or ../test/updates.*.mrt)
#   -o    also append raw results to this TSV file (for history across releases)
#
# For each REF and task, reports the median of: wall time, CPU time (user+sys),
# max RSS, number of GC cycles, and the approximate total bytes allocated
# (from GODEBUG=gctrace=1: heap at each GC start minus live heap after the previous).
#
# Example:
#   test/perf/bench.sh -n 5 -f ~/data/updates.20230301.0000.mrt v0.25.0 v0.26.0 work

set -e
RUNS=3
FILE="${BGPIPE_BENCH_FILE:-}"
OUT=""
while getopts "n:f:o:" opt; do
	case $opt in
	n) RUNS="$OPTARG" ;;
	f) FILE="$OPTARG" ;;
	o) OUT="$OPTARG" ;;
	*) sed -n '3,20p' "$0"; exit 1 ;;
	esac
done
shift $((OPTIND - 1))
[ $# -gt 0 ] || { sed -n '3,20p' "$0"; exit 1; }

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
if [ -z "$FILE" ]; then
	FILE="$(ls "$REPO"/../test/updates.*.mrt 2>/dev/null | head -1)"
fi
[ -r "$FILE" ] || { echo "input file not found, use -f" >&2; exit 1; }
FILE="$(cd "$(dirname "$FILE")" && pwd)/$(basename "$FILE")"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"; git -C "$REPO" worktree prune' EXIT

# the benchmark tasks: name -> bgpipe arguments, in ARGS
TASKS="json mrt grep"
task_args() {
	case $1 in
	json) ARGS=(-- read "$FILE" -- write --format json /dev/null) ;;
	mrt) ARGS=(-- read "$FILE" -- write --format mrt /dev/null) ;;
	grep) ARGS=(-- read "$FILE" -- grep 'prefix ~ 8.0.0.0/8 || aspath_len > 5' -- write --format json /dev/null) ;;
	esac
}

# max RSS in KiB and CPU seconds, from /usr/bin/time (macOS -l, Linux -v)
if /usr/bin/time -l true >/dev/null 2>&1; then
	TIMEFLAG=-l
else
	TIMEFLAG=-v
fi

build() { # ref -> $TMP/bgpipe-ref
	local ref="$1" safe="${1//\//_}" bin="$TMP/bgpipe-${1//\//_}"
	if [ "$ref" = "work" ]; then
		cat >"$TMP/go.work" <<-EOF
		go 1.26.0
		use $REPO
		use $REPO/../bgpfix
		EOF
		(cd "$REPO" && GOWORK="$TMP/go.work" go build -o "$bin" .)
	else
		git -C "$REPO" worktree add -q --detach "$TMP/src-$safe" "$ref"
		(cd "$TMP/src-$safe" && GOWORK=off go build -o "$bin" .)
	fi
}

run() { # bin task -> prints: wall cpu rss_mb gcs alloc_mb
	local bin="$1"
	task_args "$2"
	GODEBUG=gctrace=1 /usr/bin/time $TIMEFLAG "$bin" "${ARGS[@]}" >/dev/null 2>"$TMP/log" || {
		echo "run failed: $bin ${ARGS[*]}" >&2; tail -5 "$TMP/log" >&2; exit 1; }
	awk -v flag="$TIMEFLAG" '
		/^gc [0-9]+ @/ {
			gcs++
			for (i = 1; i <= NF; i++) if ($i ~ /->.*->/) {
				split($i, h, "->"); alloc += h[1] - live; live = h[3]
			}
		}
		flag == "-l" && / real / { wall = $1; cpu = $3 + $5 }
		flag == "-l" && /maximum resident set size/ { rss = $1 / 1048576 }
		flag == "-v" && /Elapsed \(wall clock\)/ { n = split($NF, t, ":"); wall = t[n] + (n > 1 ? t[n-1] * 60 : 0) }
		flag == "-v" && /User time/ { cpu += $NF }
		flag == "-v" && /System time/ { cpu += $NF }
		flag == "-v" && /Maximum resident set size/ { rss = $NF / 1024 }
		END { printf "%.2f %.2f %.0f %d %.0f\n", wall, cpu, rss, gcs, alloc + live }
	' "$TMP/log"
}

median() { # column number, from stdin
	sort -n -k"$1" | awk -v c="$1" '{ v[NR] = $c } END { print v[int((NR + 1) / 2)] }'
}

echo "# input: $FILE ($(du -h "$FILE" | cut -f1)), $RUNS runs, median"
echo
echo "| ref | task | wall s | cpu s | max RSS MB | GCs | ~alloc MB |"
echo "|-----|------|-------:|------:|-----------:|----:|----------:|"
for ref in "$@"; do
	build "$ref"
	for task in $TASKS; do
		: >"$TMP/runs"
		for _ in $(seq "$RUNS"); do
			run "$TMP/bgpipe-${ref//\//_}" "$task" >>"$TMP/runs"
		done
		read -r wall cpu rss gcs alloc <<<"$(for c in 1 2 3 4 5; do median $c <"$TMP/runs"; done | tr '\n' ' ')"
		echo "| $ref | $task | $wall | $cpu | $rss | $gcs | $alloc |"
		if [ -n "$OUT" ]; then
			rev="$ref"
			[ "$ref" = "work" ] && rev="HEAD+dirty"
			printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$(date -u +%FT%TZ)" \
				"$ref" "$(git -C "$REPO" rev-parse --short "${rev%+dirty}")" \
				"$(basename "$FILE")" "$task" "$wall" "$cpu" "$rss" "$gcs" "$alloc" "$RUNS" >>"$OUT"
		fi
	done
done
