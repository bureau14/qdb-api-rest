#!/usr/bin/env bash
# The stress (docs/e2e.md, Stress definition): shell and curl, run by
# hand, asserted as behaviour and never as a timing. It has the drain
# step. The budget step joins with the observability milestone.
#
#   stress.sh drain --name <cluster> --http <url> --pid-file <file> --log <file> --tool <e2etool> --out <dir> --rows N --seed S --limit-rate RATE --exit-window SECONDS [--login-body <file>]
#
# One call is one cluster and one server, because the step ends the
# server it was given with SIGTERM and the Makefile starts the next. The
# Makefile is the only source of URLs, ports, paths and the parameters,
# and passes them as options. The login body is the user security file
# on the secure cluster and absent on the insecure one, where the
# anonymous pair is written here. common.sh supplies the request, login,
# create, ingest and compare helpers both drivers share, and TZ=UTC.
# Working files go to the given out directory. The script needs bash,
# curl, jq and cmp.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

require_command curl
require_command jq
require_command cmp

NAME="" HTTP="" PID_FILE="" LOG="" TOOL="" OUT="" ROWS="" SEED="" LIMIT_RATE="" EXIT_WINDOW="" LOGIN_BODY=""
TABLE=e2e_stress

usage() {
    die "usage: stress.sh drain --name <cluster> --http <url> --pid-file <file> --log <file> --tool <e2etool> --out <dir> --rows N --seed S --limit-rate RATE --exit-window SECONDS [--login-body <file>]"
}

parse_args() {
    [[ "${1:-}" == drain ]] || usage
    shift
    while (( $# > 0 )); do
        case "$1" in
            --name)        NAME="$2"; shift 2 ;;
            --http)        HTTP="$2"; shift 2 ;;
            --pid-file)    PID_FILE="$2"; shift 2 ;;
            --log)         LOG="$2"; shift 2 ;;
            --tool)        TOOL="$2"; shift 2 ;;
            --out)         OUT="$2"; shift 2 ;;
            --rows)        ROWS="$2"; shift 2 ;;
            --seed)        SEED="$2"; shift 2 ;;
            --limit-rate)  LIMIT_RATE="$2"; shift 2 ;;
            --exit-window) EXIT_WINDOW="$2"; shift 2 ;;
            --login-body)  LOGIN_BODY="$2"; shift 2 ;;
            *)             usage ;;
        esac
    done
    [[ -n "$NAME" && -n "$HTTP" && -f "$PID_FILE" && -f "$LOG" && -x "$TOOL" && -n "$OUT" && -n "$ROWS" && -n "$SEED" && -n "$LIMIT_RATE" && -n "$EXIT_WINDOW" ]] || usage
}

# slow_read <out> <name> <url> <token> <table> <rate>: GET the table as
# CSV in the background under curl --limit-rate, the body and the
# headers to <out>/<name>.*, and curl's pid in SLOW_PID. The status lands
# in <out>/<name>.status when curl ends, and an empty status file is how
# the step knows the read is still open. The client is the slow side, so
# the server's write blocks on it and the stream is in flight when the
# signal arrives.
slow_read() {
    local out="$1" name="$2" url="$3" token="$4" table="$5" rate="$6"
    : > "$out/$name.status"
    curl -sS -g --limit-rate "$rate" -o "$out/$name.body" -D "$out/$name.headers" -w '%{http_code}' \
        -H "Authorization: Bearer $token" -H 'Accept: text/csv' "$url/api/v2/tables/$table/rows" \
        > "$out/$name.status" 2> "$out/$name.err" &
    SLOW_PID=$!
}

# wait_for_first_byte <file> <attempts>: wait until the file is
# non-empty, polling every tenth of a second, and fail after the
# attempts. The attempts are a failure guard for a server that never
# answers, not an assertion on time.
wait_for_first_byte() {
    local file="$1" attempts="$2" i
    for i in $(seq 1 "$attempts"); do
        [[ -s "$file" ]] && return 0
        sleep 0.1
    done
    log_error "[FAIL] slow read: no byte arrived within $attempts attempts"
    return 1
}

# probe_refused <url> <server_pid> <status_file> <attempts>: connect to
# the HTTP listener until the connection is refused, while the server is
# still alive and the slow read still open, and fail after the attempts
# or when either has ended.
probe_refused() {
    # A connect that still succeeds is retried and not failed, because
    # the listener closes a moment after the signal, and the assertion
    # is that it becomes refused while the stream is open. The order of
    # the checks is the assertion:
    #
    #  1. both the server and the read are alive, else fail;
    #  2. one connect; curl exit 7 is the refusal;
    #  3. anything else goes round, up to the attempts.
    local url="$1" server_pid="$2" status_file="$3" attempts="$4" i rc=0
    for i in $(seq 1 "$attempts"); do
        # 1. A refusal from a dead server, or after the read has ended,
        #    proves nothing about the drain.
        kill -0 "$server_pid" 2>/dev/null || { log_error "[FAIL] refused: the server exited first"; return 1; }
        [[ ! -s "$status_file" ]] || { log_error "[FAIL] refused: the slow read ended first"; return 1; }
        # 2. The liveness path with no token, because that is the path a
        #    load balancer polls. --max-time bounds a connect that hangs,
        #    which is not the answer either way.
        rc=0
        curl -sS -o /dev/null --max-time 2 "$url/api/status/liveness" 2>/dev/null || rc=$?
        if (( rc == 7 )); then
            log_info "[OK] refused: a new connection is refused while the slow read is open"
            return 0
        fi
        # 3. Still accepting, or another error: go round.
        sleep 0.1
    done
    log_error "[FAIL] refused: the connection was not refused within $attempts attempts (last curl exit $rc)"
    return 1
}

# drain_step <out> <url> <token> <table> <csv> <pid_file> <log> <rate>
# <exit_window>: the drain step of docs/e2e.md: one slow read across
# SIGTERM, a refused connection while it runs, the body byte for byte,
# and a clean shutdown in the log. Returns 1 on the first failed
# assertion.
drain_step() {
    # The signal goes out only once the first byte has arrived, so the
    # stream is provably in flight, and the step waits for the server to
    # exit by itself, because a second SIGTERM would kill it mid-drain
    # (wait_for_exit says why). The steps, in the order that is the test:
    #
    #  1. start the slow read;
    #  2. wait for its first byte;
    #  3. read the pid, confirm it is alive, send one SIGTERM;
    #  4. the refused connection, while the read is open;
    #  5. the read ends with 200 and the generated bytes;
    #  6. the server exits on its own within the window;
    #  7. the log says the shutdown was clean.
    local out="$1" url="$2" token="$3" table="$4" csv="$5" pid_file="$6" log="$7" rate="$8" exit_window="$9"
    local name=slow pid status
    # 1. the slow read
    slow_read "$out" "$name" "$url" "$token" "$table" "$rate"
    # 2. its first byte, so the stream is in flight before the signal
    wait_for_first_byte "$out/$name.body" 600 || return 1
    # 3. one SIGTERM to a server that is up; read_pid_file dies on a pid
    #    that is not running
    pid=$(read_pid_file "$pid_file")
    kill "$pid"
    log_info "$NAME: SIGTERM sent to $pid with the slow read open"
    # 4. refused while the read is open
    probe_refused "$url" "$pid" "$out/$name.status" 600 || return 1
    # 5. the read ends, and the body is the generated CSV with no
    #    decoder, because the reader's CSV is the generated bytes as
    #    they are
    wait "$SLOW_PID" || true
    status=$(cat "$out/$name.status")
    expect 200 "$status" "$out" "$name" || return 1
    compare_csv "$out" "$name" csv "$table" "$out/$name.body" "$csv" raw "$TOOL" || return 1
    log_info "[OK] $NAME: the slow read completed across the drain, byte for byte"
    # 6. the server exits on its own
    wait_for_exit "$pid" "$exit_window"
    # 7. the log's last word: a fixed string, as the round trip's warning
    #    check greps
    grep -qF 'shutdown complete' "$log" || { log_error "[FAIL] $NAME: no 'shutdown complete' in $log"; return 1; }
    ! grep -qF 'server failed' "$log" || { log_error "[FAIL] $NAME: 'server failed' in $log"; return 1; }
    log_info "[OK] $NAME: the server drained and exited cleanly"
}

# run_cluster: login, the table, the generated rows ingested as CSV,
# then the drain step. The table is e2e_stress on both clusters and is
# created delete-first, as the round trip's are.
run_cluster() {
    #  1. login as the round trip logs in;
    #  2. gen once with the table name, because $table is a column of
    #     the body;
    #  3. create and ingest as CSV, one format because the formats are
    #     the round trip's question and not the stress's;
    #  4. the drain step.
    local token rows
    log_info "$NAME: $HTTP, rows=$ROWS seed=$SEED limit-rate=$LIMIT_RATE"
    # 1. login
    token=$(login "$OUT" "$HTTP" "$LOGIN_BODY") || return 1
    # 2. the rows
    rows=$("$TOOL" gen --rows "$ROWS" --seed "$SEED" --out "$OUT" --table "$TABLE") || die "$NAME: e2etool gen failed"
    # 3. create and ingest as CSV
    create_table "$OUT" "$HTTP" "$token" "$TABLE" || return 1
    ingest "$OUT" "$HTTP" "$token" "$TABLE" text/csv "$OUT/rows.csv" "$rows" || return 1
    log_info "$NAME: $TABLE, $rows rows in as csv"
    # 4. the drain step
    drain_step "$OUT" "$HTTP" "$token" "$TABLE" "$OUT/rows.csv" "$PID_FILE" "$LOG" "$LIMIT_RATE" "$EXIT_WINDOW"
}

main() {
    parse_args "$@"
    mkdir -p "$OUT"
    if [[ -z "$LOGIN_BODY" ]]; then
        LOGIN_BODY="$OUT/anonymous.json"
        printf '{"username":"","secret_key":""}' > "$LOGIN_BODY"
    fi
    run_cluster || die "stress drain step: $NAME FAILED (seed=$SEED)"
    log_info "stress drain step: $NAME passed (seed=$SEED)"
}

main "$@"
