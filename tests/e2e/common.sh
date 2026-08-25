#!/usr/bin/env bash
# Shared helpers for the qdb-api-rest e2e harness (docs/e2e-plan.md).
# Lineage: qdb-nats-connector examples/common.sh (ADR-007), adapted.
# Sourced by legacy.sh, tools/*.sh and the Makefile's inline shell.
#
# Reads top to bottom: logging, environment, process control, qdb helpers,
# CSV comparison. Nothing here starts qdbd -- it is a persistent service
# owned by scripts/tests/setup/start-services.sh.

set -euo pipefail

# ---------------------------------------------------------------- logging

log_info()  { echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') $*"; }
log_error() { echo "[ERROR] $(date '+%Y-%m-%d %H:%M:%S') $*" >&2; }
log_debug() { [[ "${DEBUG:-0}" == "1" ]] && echo "[DEBUG] $(date '+%Y-%m-%d %H:%M:%S') $*" || true; }

die() {
    log_error "$1"
    exit "${2:-1}"
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || die "Required command '$1' not found"
}

# ------------------------------------------------------------ environment

case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) EXE_EXT=".exe" ;;
    *)                    EXE_EXT="" ;;
esac
export EXE_EXT

# Resolve paths relative to this file, never to the caller's PWD.
E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$E2E_DIR/../.." && pwd)"
export E2E_DIR REPO_ROOT

# Explicit tool paths (PATH is not mutated). The qdb tree is the extracted
# QuasarDB distribution at <repo>/qdb (fetched, gitignored). Overridable.
export QDB_URI="${QDB_URI:-qdb://127.0.0.1:2836}"
export QDB_DIR="${QDB_DIR:-$REPO_ROOT/qdb}"
export QDB_BIN_DIR="${QDB_BIN_DIR:-$QDB_DIR/bin}"
export QDBD="${QDBD:-$QDB_BIN_DIR/qdbd$EXE_EXT}"
export QDBSH="${QDBSH:-$QDB_BIN_DIR/qdbsh$EXE_EXT}"
export QDB_EXPORT="${QDB_EXPORT:-$QDB_BIN_DIR/qdb_export$EXE_EXT}"
export QDB_IMPORT="${QDB_IMPORT:-$QDB_BIN_DIR/qdb_import$EXE_EXT}"

# qdbsh writes qdbsh.log* into its log directory (default: cwd). Keep that
# noise out of the tree; every qdbsh call goes through this wrapper.
export QDBSH_LOG_DIR="${QDBSH_LOG_DIR:-${TMPDIR:-/tmp}/qdb-e2e-qdbsh-logs}"
qdbsh() {
    mkdir -p "$QDBSH_LOG_DIR"
    "$QDBSH" --log-directory "$QDBSH_LOG_DIR" "$@"
}

# Set the loader path explicitly: the shipped tools' rpath to ../lib is
# not honoured on every platform.
export LD_LIBRARY_PATH="$QDB_DIR/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export DYLD_LIBRARY_PATH="$QDB_DIR/lib${DYLD_LIBRARY_PATH:+:$DYLD_LIBRARY_PATH}"

# Legacy JSON renders timestamps in the server's local time zone. Goldens are
# captured and replayed under UTC so they are portable across machines.
export TZ=UTC

# ---------------------------------------------------------- process control

# TCP probe on localhost. Usage: check_service_running <name> <port> [timeout]
check_service_running() {
    local name="$1" port="$2" timeout="${3:-5}"
    if command -v timeout >/dev/null 2>&1; then
        timeout "$timeout" bash -c "echo >/dev/tcp/127.0.0.1/$port" 2>/dev/null && return 0
    else
        nc -z 127.0.0.1 "$port" 2>/dev/null && return 0
    fi
    log_debug "$name is not answering on port $port"
    return 1
}

# Poll a port until it answers. Usage: wait_for_port <name> <port> [seconds]
wait_for_port() {
    local name="$1" port="$2" seconds="${3:-60}" elapsed=0
    while (( elapsed < seconds )); do
        check_service_running "$name" "$port" 1 && return 0
        sleep 1
        elapsed=$((elapsed + 1))
    done
    log_error "$name did not answer on port $port within ${seconds}s"
    return 1
}

require_qdbd() {
    local port="${QDB_URI##*:}"
    check_service_running qdbd "$port" || die "qdbd is not answering on $QDB_URI; run: bash scripts/tests/setup/start-services.sh"
}

# Atomic pidfile (hardlink create). Usage: write_pid_file <file> [pid]
write_pid_file() {
    local pid_file="$1" pid="${2:-$$}" tmp="${1}.tmp.$$"
    echo "$pid" > "$tmp"
    if ln "$tmp" "$pid_file" 2>/dev/null; then
        rm -f "$tmp"
        return 0
    fi
    rm -f "$tmp"
    local existing
    if existing=$(read_pid_file "$pid_file" 2>/dev/null); then
        die "PID file $pid_file already exists with running process $existing"
    fi
    log_debug "Removing stale PID file $pid_file"
    rm -f "$pid_file"
    echo "$pid" > "$tmp"
    ln "$tmp" "$pid_file" 2>/dev/null || { rm -f "$tmp"; die "Failed to create PID file $pid_file"; }
    rm -f "$tmp"
}

# Prints the pid; dies if absent, malformed, or not running.
read_pid_file() {
    local pid_file="$1" pid
    [[ -f "$pid_file" ]] || die "PID file $pid_file does not exist"
    pid=$(cat "$pid_file")
    [[ "$pid" =~ ^[0-9]+$ ]] || die "Invalid PID in $pid_file: $pid"
    kill -0 "$pid" 2>/dev/null || die "Process $pid from $pid_file is not running"
    echo "$pid"
}

cleanup_pid_file() { rm -f "$1"; }

# Start a background process, record its pid, wait for its port.
# Usage: start_server <pidfile> <logfile> <port> <cmd...>
start_server() {
    local pid_file="$1" log_file="$2" port="$3"
    shift 3
    "$@" > "$log_file" 2>&1 &
    write_pid_file "$pid_file" $!
    if ! wait_for_port "$(basename "$1")" "$port" 60; then
        tail -50 "$log_file" >&2 || true
        stop_server "$pid_file"
        die "server did not start; log: $log_file"
    fi
}

# SIGTERM, wait up to 10s, then SIGKILL. Usage: stop_server <pidfile>
stop_server() {
    local pid_file="$1" pid i
    [[ -f "$pid_file" ]] || return 0
    pid=$(cat "$pid_file")
    if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null || true
        for i in $(seq 1 100); do
            kill -0 "$pid" 2>/dev/null || break
            sleep 0.1
        done
        kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
    fi
    cleanup_pid_file "$pid_file"
}

# ------------------------------------------------------------ qdb helpers

# COUNT(*) of one table via qdbsh; prints 0 when the table is missing.
# qdbsh output:  header / --- divider / value row (with thousands commas).
# Usage: count_qdb_rows <table> [cluster_uri]
count_qdb_rows() {
    local table="$1" uri="${2:-$QDB_URI}" count
    count=$(qdbsh --cluster "$uri" -c "SELECT COUNT(*) FROM \"$table\"" 2>/dev/null \
        | awk '/^-{3,}$/{getline; print $1; exit}' | tr -d ',')
    echo "${count:-0}"
}

# Run a file of qdbsh statements, one per line, stopping at the first error
# (qdbsh exits non-zero on query errors). Blank lines and lines starting with
# -- are skipped; DROP TABLE of a missing table and attach_tag of an already
# attached tag are tolerated (idempotent re-seeding).
run_qdbsh_file() {
    local file="$1" line out
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" || "$line" == --* ]] && continue
        if ! out=$(qdbsh --cluster "$QDB_URI" -c "$line" 2>&1); then
            [[ "$line" == DROP\ TABLE* ]] && continue
            [[ "$line" == attach_tag* && "$out" == *"already marked"* ]] && continue
            die "qdbsh failed on: $line"$'\n'"$out"
        fi
    done < "$file"
}

# Shard size of a table in milliseconds (SHOW TABLE, 5th column).
# Usage: table_shard_ms <table> [cluster_uri]
table_shard_ms() {
    local table="$1" uri="${2:-$QDB_URI}" ms
    ms=$(qdbsh --cluster "$uri" -c "SHOW TABLE \"$table\"" | awk '/^-{3,}$/{getline; print $5; exit}' | tr -d ',')
    [[ "$ms" =~ ^[0-9]+$ ]] || die "could not read shard size of $table"
    echo "$ms"
}

# First and last $timestamp of a table, second precision, as "<first> <last>".
# qdb refuses to select only special columns, so the first regular column
# rides along. Usage: table_time_range <table> [cluster_uri]
table_time_range() {
    local table="$1" uri="${2:-$QDB_URI}" col first last
    col=$(qdbsh --cluster "$uri" -c "SHOW TABLE \"$table\"" | awk '/^-{3,}$/{getline; print $2; exit}')
    first=$(qdbsh --cluster "$uri" -c "SELECT \$timestamp, $col FROM \"$table\" LIMIT 1" \
        | awk '/^-{3,}$/{getline; print $1; exit}' | cut -c1-19)
    last=$(qdbsh --cluster "$uri" -c "SELECT \$timestamp, $col FROM \"$table\" ORDER BY \$timestamp DESC LIMIT 1" \
        | awk '/^-{3,}$/{getline; print $1; exit}' | cut -c1-19)
    [[ -n "$first" && -n "$last" ]] || die "could not determine the time range of $table"
    echo "$first $last"
}

# Epoch-seconds conversions that work with GNU and BSD date.
to_epoch()   { date -u -d "$1" +%s 2>/dev/null || date -u -j -f "%Y-%m-%dT%H:%M:%S" "$1" +%s; }
from_epoch() { date -u -d "@$1" +%Y-%m-%dT%H:%M:%S 2>/dev/null || date -u -r "$1" +%Y-%m-%dT%H:%M:%S; }

# qdb_export a whole table to CSV (no header row; qdb_export convention).
# qdb_export reads through the bulk reader, whose result must fit the client
# network input buffer (125 MiB default, no flag to raise it), so large tables
# are exported one shard-sized half-open range at a time and concatenated;
# ranges are ordered, so the result equals a single export. The optional
# config path receives the qdb_import config (written by the first chunk).
# Usage: export_table_csv <table> <csv> [cluster_uri] [import_config]
export_table_csv() {
    local table="$1" csv="$2" uri="${3:-$QDB_URI}" config="${4:-}"
    local shard_s range first last start end chunk_end
    local -a config_args=()
    [[ -n "$config" ]] && config_args=(--config "$config")
    shard_s=$(( $(table_shard_ms "$table" "$uri") / 1000 ))
    range=$(table_time_range "$table" "$uri"); first=${range% *}; last=${range#* }
    start=$(( $(to_epoch "$first") / shard_s * shard_s ))
    end=$(( $(to_epoch "$last") + 1 ))
    log_info "exporting $table [$first .. $last] in ${shard_s}s chunks"
    : > "$csv"
    while (( start < end )); do
        chunk_end=$(( start + shard_s ))
        "$QDB_EXPORT" --ts "$table" -f "$csv.chunk" -c "$uri" "${config_args[@]}" \
            --start-date "$(from_epoch "$start")" --end-date "$(from_epoch "$chunk_end")" >/dev/null \
            || die "qdb_export of $table failed for chunk starting $(from_epoch "$start")"
        cat "$csv.chunk" >> "$csv"
        config_args=()
        start=$chunk_end
    done
    rm -f "$csv.chunk"
}

# ---------------------------------------------------------- CSV comparison

# Row-by-row CSV compare with numeric tolerance (CSV_ABS_TOLERANCE, default
# 0.001) on fields numeric on both sides; everything else byte-exact. O(1)
# memory: awk streams <actual> and pulls the matching <golden> row by getline.
# Prints the first 10 differences on failure. Usage: compare_csv <actual> <golden> <label>
compare_csv() {
    local actual="$1" golden="$2" label="$3" diff_output
    if diff_output=$(awk -v golden="$golden" -v tol="${CSV_ABS_TOLERANCE:-0.001}" '
        function abs(x)    { return x < 0 ? -x : x }
        function is_num(s) { return s ~ /^[+-]?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?$/ }
        BEGIN { FS = ","; diffs = 0; shown = 0; max_show = 10 }
        {
            if ((getline gline < golden) <= 0) {
                if (shown < max_show) { printf("row %d: present in actual, missing in golden\n", NR); shown++ }
                diffs++; next
            }
            if ($0 == gline) next
            gn = split(gline, g, ",")
            if (NF != gn) {
                if (shown < max_show) { printf("row %d: field count differs (actual=%d, golden=%d)\n", NR, NF, gn); shown++ }
                diffs++; next
            }
            for (i = 1; i <= NF; i++) {
                if ($i == g[i]) continue
                if (is_num($i) && is_num(g[i])) {
                    if (abs(($i + 0) - (g[i] + 0)) <= tol) continue
                    if (shown < max_show) { printf("row %d col %d: |%s - %s| > %s\n", NR, i, $i, g[i], tol); shown++ }
                } else {
                    if (shown < max_show) { printf("row %d col %d: [%s] != [%s]\n", NR, i, $i, g[i]); shown++ }
                }
                diffs++
            }
        }
        END {
            if ((getline gline < golden) > 0) {
                if (shown < max_show) { printf("row %d: golden has more rows than actual\n", NR + 1); shown++ }
                diffs++
            }
            close(golden)
            exit (diffs > 0) ? 1 : 0
        }' "$actual"); then
        log_info "[OK] $label"
        return 0
    fi
    log_error "[FAIL] $label"
    printf '%s\n' "$diff_output" >&2
    return 1
}

# sha256 of a file, portable (coreutils sha256sum or BSD shasum).
sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}
