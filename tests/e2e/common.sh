#!/usr/bin/env bash
# Shared helpers for the qdb-api-rest e2e harness (docs/e2e.md). They are
# adapted from qdb-nats-connector's examples/common.sh (ADR-007) and are
# sourced by the Makefile's inline shell.
#
# The file reads top to bottom: logging, environment, process control. Nothing here starts qdbd. It is a persistent service owned
# by scripts/tests/setup/start-services.sh.

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

# Explicit tool paths, so PATH is not mutated. The qdb tree is the extracted
# QuasarDB distribution at <repo>/qdb (fetched, gitignored). Every path can
# be overridden.
export QDB_URI="${QDB_URI:-qdb://127.0.0.1:2836}"
export QDB_DIR="${QDB_DIR:-$REPO_ROOT/qdb}"
export QDB_BIN_DIR="${QDB_BIN_DIR:-$QDB_DIR/bin}"
export QDBD="${QDBD:-$QDB_BIN_DIR/qdbd$EXE_EXT}"
export QDBSH="${QDBSH:-$QDB_BIN_DIR/qdbsh$EXE_EXT}"
export QDB_EXPORT="${QDB_EXPORT:-$QDB_BIN_DIR/qdb_export$EXE_EXT}"
export QDB_IMPORT="${QDB_IMPORT:-$QDB_BIN_DIR/qdb_import$EXE_EXT}"

# qdbsh writes qdbsh.log* into its log directory, which defaults to the
# working directory. This wrapper keeps that noise out of the tree, so
# every qdbsh call goes through it.
export QDBSH_LOG_DIR="${QDBSH_LOG_DIR:-${TMPDIR:-/tmp}/qdb-e2e-qdbsh-logs}"
qdbsh() {
    mkdir -p "$QDBSH_LOG_DIR"
    "$QDBSH" --log-directory "$QDBSH_LOG_DIR" "$@"
}

# Set the loader path explicitly, because the shipped tools' rpath to
# ../lib is not honoured on every platform.
export LD_LIBRARY_PATH="$QDB_DIR/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export DYLD_LIBRARY_PATH="$QDB_DIR/lib${DYLD_LIBRARY_PATH:+:$DYLD_LIBRARY_PATH}"

# Every server the harness starts runs under UTC, so a response that
# carries a local-time timestamp is portable across machines.
export TZ=UTC

# ---------------------------------------------------------- process control

# Probe a TCP port on localhost. Usage: check_service_running <name> <port> [timeout]
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

# Write a pidfile atomically, through a hardlink create. Usage: write_pid_file <file> [pid]
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

# Print the pid. Die if the file is absent or malformed, or its process is
# not running.
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

# Send SIGTERM, wait up to 10s, then send SIGKILL. Usage: stop_server <pidfile>
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
