#!/usr/bin/env bash
# Shared helpers for the qdb-api-rest e2e harness (docs/e2e.md). They are
# adapted from qdb-nats-connector's examples/common.sh (ADR-007) and are
# sourced by the Makefile's inline shell.
#
# The file reads top to bottom: logging, environment, process control,
# requests. Nothing here starts qdbd. It is a persistent service owned
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

# Send SIGTERM, wait up to 10s, then send SIGKILL. The wait sits above the
# default shutdown grace (listen.shutdown_grace, 8s), so a clean drain of
# a server at the default is never cut short. A server started under a
# longer grace is stopped by the step that configured it. The window does
# not read the grace from an argument, because no step sets one.
# Usage: stop_server <pidfile>
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

# ---------------------------------------------------------------- requests

# The request helpers both drivers share: one curl request with its body
# and headers kept, the status assertion, the login, the table create,
# the ingest and the CSV compare. Each takes its state as arguments, so
# a driver sources this file and owns no global for them.

# request <out_dir> <name> <curl args...>: one request, its body in
# <out_dir>/<name>.body, its headers in <out_dir>/<name>.headers, its
# status on stdout. -g keeps [] literal, and --compressed is deliberately
# not used, because what a plain client sends is no Accept-Encoding at
# all. The header dump is what check_response asserts content-type and
# content-encoding from, because a -w format could not say that a
# header is absent.
request() {
    local out="$1" name="$2"
    shift 2
    curl -sS -g -o "$out/$name.body" -D "$out/$name.headers" -w '%{http_code}' "$@" || die "curl failed: $name"
}

# expect <status> <got> <out_dir> <name>: the status is the whole
# assertion; on a mismatch the body is printed, because it is a problem
# document that says who failed.
expect() {
    local want="$1" got="$2" out="$3" name="$4"
    if [[ "$got" != "$want" ]]; then
        log_error "[FAIL] $name: status $got, want $want"
        head -c 1024 "$out/$name.body" >&2 || true
        echo >&2
        return 1
    fi
}

# login <out_dir> <url> <body_file>: POST /api/v2/auth/login with the
# body file as it is, which is the anonymous pair on the insecure cluster
# and the user security file verbatim on the secure one, since the file
# has the login body's shape. A login is checked by shape (ADR-0014 3),
# a non-empty access_token and refresh_token. Then refresh exchanges the
# refresh token for a new grant, and session reads the caller back under
# the new access token. The new access token is printed and authenticates
# every later step, so the round trip proves a refreshed token works.
# Logout has no step, because it changes nothing a later step could
# observe (docs/e2e.md).
login() {
    local out="$1" url="$2" body="$3" status token refresh username
    # 1. login: 200, access_token and refresh_token non-empty
    status=$(request "$out" login -X POST -H 'Content-Type: application/json' --data-binary "@$body" "$url/api/v2/auth/login")
    expect 200 "$status" "$out" login || return 1
    token=$(jq -r '.access_token // empty' "$out/login.body")
    [[ -n "$token" ]] || { log_error "[FAIL] login: no access_token in body"; return 1; }
    refresh=$(jq -r '.refresh_token // empty' "$out/login.body")
    [[ -n "$refresh" ]] || { log_error "[FAIL] login: no refresh_token in body"; return 1; }
    # 2. refresh: 200 and the same shape, the new grant
    status=$(request "$out" refresh -X POST -H 'Content-Type: application/json' --data-binary "{\"refresh_token\":\"$refresh\"}" "$url/api/v2/auth/refresh")
    expect 200 "$status" "$out" refresh || return 1
    token=$(jq -r '.access_token // empty' "$out/refresh.body")
    [[ -n "$token" ]] || { log_error "[FAIL] refresh: no access_token in body"; return 1; }
    refresh=$(jq -r '.refresh_token // empty' "$out/refresh.body")
    [[ -n "$refresh" ]] || { log_error "[FAIL] refresh: no refresh_token in body"; return 1; }
    # 3. session under the new access token: 200, username equals the
    #    body file's ("" on the insecure cluster)
    status=$(request "$out" session -H "Authorization: Bearer $token" "$url/api/v2/session")
    expect 200 "$status" "$out" session || return 1
    username=$(jq -r '.username' "$out/session.body")
    [[ "$username" == "$(jq -r '.username' "$body")" ]] || { log_error "[FAIL] session: username $username, want $(jq -r '.username' "$body")"; return 1; }
    # 4. the refreshed access token is the round trip's token
    echo "$token"
}

# create_body <table>: the create body of a round trip table, one column of
# every type named v_<type>, the names e2etool's roundTripSchema spells, with
# the symtable named after the table, sharded by a day (shard_size is in
# milliseconds and required).
create_body() {
    local table="$1"
    cat <<EOF
{"name":"$table","shard_size":86400000,"columns":[
 {"name":"v_int64","type":"int64"},
 {"name":"v_double","type":"double"},
 {"name":"v_string","type":"string"},
 {"name":"v_symbol","type":"symbol","symtable":"${table}_v_symbol"},
 {"name":"v_blob","type":"blob"},
 {"name":"v_timestamp","type":"timestamp"}]}
EOF
}

# create_table <out_dir> <url> <token> <table>: DELETE then POST
# /api/v2/tables. The table is dropped first so the round trip is idempotent,
# and a 404 on the delete is the clean state. The create must answer 201.
create_table() {
    local out="$1" url="$2" token="$3" table="$4" status
    status=$(request "$out" "$table.delete" -X DELETE -H "Authorization: Bearer $token" "$url/api/v2/tables/$table")
    [[ "$status" == 204 || "$status" == 404 ]] || { expect 204 "$status" "$out" "$table.delete"; return 1; }
    status=$(request "$out" "$table.create" -X POST -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
        --data-binary "$(create_body "$table")" "$url/api/v2/tables")
    expect 201 "$status" "$out" "$table.create"
}

# ingest <out_dir> <url> <token> <table> <media_type> <file> <rows>:
# POST /api/v2/rows with the file as a body of that type. The answer's
# rows must equal the count the tool wrote, edge rows included, and the
# tool is the one writer of that number.
ingest() {
    local out="$1" url="$2" token="$3" table="$4" type="$5" file="$6" rows="$7" status got
    status=$(request "$out" "$table.ingest" -X POST -H "Authorization: Bearer $token" -H "Content-Type: $type" \
        --data-binary "@$file" "$url/api/v2/rows")
    expect 200 "$status" "$out" "$table.ingest" || return 1
    got=$(jq -r '.rows' "$out/$table.ingest.body")
    [[ "$got" == "$rows" ]] || { log_error "[FAIL] $table.ingest: rows $got, want $rows"; return 1; }
}

# compare_csv <out_dir> <name> <format> <table> <body_file> <csv> <csv_is> <tool>:
# the body, decoded to CSV through e2etool tocsv, compared byte for byte
# with the generated CSV (ADR-0014 2). A CSV body skips the tool when
# <csv_is> is raw: the reader's CSV is the generated bytes as they are.
# The query's CSV is not, because a bare SELECT * answers $timestamp
# before $table, so the query passes tool and the CSV goes through the
# decoder like every other format. On a mismatch the first bytes of both
# are diffed, because the seed reproduces the rest. The tool is an
# argument, because both drivers share this helper and neither exports
# a global for it.
compare_csv() {
    local out="$1" name="$2" format="$3" table="$4" body="$5" csv="$6" csv_is="$7" tool="$8"
    if [[ "$format" != csv || "$csv_is" == tool ]]; then
        "$tool" tocsv --format "$format" --table "$table" < "$body" > "$out/$name.csv" \
            || { log_error "[FAIL] $name: e2etool tocsv failed"; return 1; }
        body="$out/$name.csv"
    fi
    if ! cmp -s "$csv" "$body"; then
        log_error "[FAIL] $name: CSV differs from $(basename "$csv") (generated vs read)"
        diff <(head -c 4096 "$csv") <(head -c 4096 "$body") >&2 || true
        return 1
    fi
}
