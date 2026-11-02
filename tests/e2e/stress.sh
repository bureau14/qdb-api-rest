#!/usr/bin/env bash
# The stress (docs/e2e.md, Stress definition): shell and curl, run by
# hand, asserted as behaviour and never as a timing. It has two steps,
# the budget step and the drain step.
#
#   stress.sh budget --name <cluster> --http <url> --tool <e2etool> --out <dir> --rows N --seed S --clients N [--login-body <file>]
#   stress.sh drain --name <cluster> --http <url> --pid-file <file> --log <file> --tool <e2etool> --out <dir> --rows N --seed S --limit-rate RATE --exit-window SECONDS [--login-body <file>]
#
# One call is one step on one cluster's server. The drain step ends the
# server it was given with SIGTERM, so the Makefile runs the budget step
# first on the same server and starts the next cluster's after. The
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

STEP="" NAME="" HTTP="" PID_FILE="" LOG="" TOOL="" OUT="" ROWS="" SEED="" LIMIT_RATE="" EXIT_WINDOW="" CLIENTS="" LOGIN_BODY=""
# Each step has its own table, so the budget step's ingest leaves the
# drain step's e2e_stress alone on the same server.
DRAIN_TABLE=e2e_stress
BUDGET_TABLE=e2e_stress_budget

usage() {
    die "usage: stress.sh budget --name <cluster> --http <url> --tool <e2etool> --out <dir> --rows N --seed S --clients N [--login-body <file>]
       stress.sh drain --name <cluster> --http <url> --pid-file <file> --log <file> --tool <e2etool> --out <dir> --rows N --seed S --limit-rate RATE --exit-window SECONDS [--login-body <file>]"
}

parse_args() {
    STEP="${1:-}"
    [[ "$STEP" == drain || "$STEP" == budget ]] || usage
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
            --clients)     CLIENTS="$2"; shift 2 ;;
            --login-body)  LOGIN_BODY="$2"; shift 2 ;;
            *)             usage ;;
        esac
    done
    [[ -n "$NAME" && -n "$HTTP" && -x "$TOOL" && -n "$OUT" && -n "$ROWS" && -n "$SEED" ]] || usage
    case "$STEP" in
        drain)  [[ -f "$PID_FILE" && -f "$LOG" && -n "$LIMIT_RATE" && -n "$EXIT_WINDOW" ]] || usage ;;
        budget) [[ -n "$CLIENTS" ]] || usage ;;
    esac
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

# budget_gauge <file> <state>: the value of
# qdb_rest_budget_sessions{state="<state>"} in a scraped exposition. A
# fixed-string grep, as the round trip's scrape greps, because the
# exposition is a text format the driver knows nothing else about. It
# fails when the series is absent.
budget_gauge() {
    local file="$1" state="$2"
    grep -F "qdb_rest_budget_sessions{state=\"$state\"}" "$file" | awk '{ print $2 }'
}

# client_reads <out> <url> <token> <table> <clients>: GET the table as
# CSV from <clients> curls at once through xargs -P, each body and its
# headers to <out>/client-<i>.*, in the background with xargs's pid in
# READS_PID.
client_reads() {
    local out="$1" url="$2" token="$3" table="$4" clients="$5"
    seq 1 "$clients" | xargs -P "$clients" -I{} \
        curl -sS -g -o "$out/client-{}.body" -D "$out/client-{}.headers" \
        -H "Authorization: Bearer $token" -H 'Accept: text/csv' "$url/api/v2/tables/$table/rows" \
        2> "$out/clients.err" &
    READS_PID=$!
}

# poll_budget <out> <url> <reads_pid>: scrape /metrics until the reads
# end, and fail on the first scrape whose budget in use exceeds its max.
poll_budget() {
    local out="$1" url="$2" reads_pid="$3" status in_use max polls=0
    while kill -0 "$reads_pid" 2>/dev/null; do
        status=$(request "$out" budget-metrics "$url/metrics")
        expect 200 "$status" "$out" budget-metrics || return 1
        in_use=$(budget_gauge "$out/budget-metrics.body" in_use) || { log_error "[FAIL] budget: no in_use series in the exposition"; return 1; }
        max=$(budget_gauge "$out/budget-metrics.body" max) || { log_error "[FAIL] budget: no max series in the exposition"; return 1; }
        if (( in_use > max )); then
            log_error "[FAIL] budget: $in_use sessions in use, over the budget of $max"
            return 1
        fi
        polls=$((polls + 1))
        sleep 0.1
    done
    log_info "[OK] $NAME: $polls polls, the budget in use never exceeded it"
}

# budget_step <out> <url> <token> <table> <csv> <clients>: the budget
# step of docs/e2e.md: more clients than the budget read one table at
# once while the budget gauge is polled, the gauge never exceeds the
# budget, and every client's body compares equal to the rows. Returns 1
# on the first failed assertion.
budget_step() {
    local out="$1" url="$2" token="$3" table="$4" csv="$5" clients="$6" i
    client_reads "$out" "$url" "$token" "$table" "$clients"
    poll_budget "$out" "$url" "$READS_PID" || { kill "$READS_PID" 2>/dev/null || true; return 1; }
    # xargs exits non-zero when any curl did.
    wait "$READS_PID" || { log_error "[FAIL] budget: a client read failed: $(head -c 512 "$out/clients.err")"; return 1; }
    for i in $(seq 1 "$clients"); do
        if ! head -n 1 "$out/client-$i.headers" | grep -q ' 200'; then
            log_error "[FAIL] budget: client $i answered $(head -n 1 "$out/client-$i.headers")"
            return 1
        fi
        # The reader's CSV is the generated bytes as they are, as in the
        # drain step.
        compare_csv "$out" "client-$i" csv "$table" "$out/client-$i.body" "$csv" raw "$TOOL" || return 1
    done
    log_info "[OK] $NAME: $clients clients read $table in full under the budget"
}

# run_cluster: login, the step's table, the generated rows ingested as
# CSV, then the step. The table is created delete-first, as the round
# trip's are.
run_cluster() {
    #  1. login as the round trip logs in;
    #  2. gen once with the table name, because $table is a column of
    #     the body;
    #  3. create and ingest as CSV, one format because the formats are
    #     the round trip's question and not the stress's;
    #  4. the step.
    local token rows table
    table=$([[ "$STEP" == drain ]] && echo "$DRAIN_TABLE" || echo "$BUDGET_TABLE")
    log_info "$NAME: $STEP step, $HTTP, rows=$ROWS seed=$SEED"
    # 1. login
    token=$(login "$OUT" "$HTTP" "$LOGIN_BODY") || return 1
    # 2. the rows
    rows=$("$TOOL" gen --rows "$ROWS" --seed "$SEED" --out "$OUT" --table "$table") || die "$NAME: e2etool gen failed"
    # 3. create and ingest as CSV
    create_table "$OUT" "$HTTP" "$token" "$table" || return 1
    ingest "$OUT" "$HTTP" "$token" "$table" text/csv "$OUT/rows.csv" "$rows" || return 1
    log_info "$NAME: $table, $rows rows in as csv"
    # 4. the step
    case "$STEP" in
        drain)  drain_step "$OUT" "$HTTP" "$token" "$table" "$OUT/rows.csv" "$PID_FILE" "$LOG" "$LIMIT_RATE" "$EXIT_WINDOW" ;;
        budget) budget_step "$OUT" "$HTTP" "$token" "$table" "$OUT/rows.csv" "$CLIENTS" ;;
    esac
}

main() {
    parse_args "$@"
    mkdir -p "$OUT"
    if [[ -z "$LOGIN_BODY" ]]; then
        LOGIN_BODY="$OUT/anonymous.json"
        printf '{"username":"","secret_key":""}' > "$LOGIN_BODY"
    fi
    run_cluster || die "stress $STEP step: $NAME FAILED (seed=$SEED)"
    log_info "stress $STEP step: $NAME passed (seed=$SEED)"
}

main "$@"
