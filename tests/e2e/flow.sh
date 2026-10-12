#!/usr/bin/env bash
# The v2 e2e flow (docs/e2e.md, "The v2 flow"). It proves the built
# binary over HTTP like a client: login, create a table, ingest the
# generated rows, read them back. The expected value of every step is
# the generated input, so nothing is captured and nothing is audited
# (ADR-0014). Error rows are Go tests, never steps here.
#
#   flow.sh run --insecure-url <url> --secure-url <url> --tool <e2etool> [--rows N] [--seed S]
#
# The Makefile is the only source of URLs, ports and paths, and passes
# them as options. common.sh supplies the helpers and TZ=UTC. Working
# files go to actual/flow/<cluster>/. The script needs bash, curl, jq
# and cmp.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

require_command curl
require_command jq
require_command cmp

TABLE="e2e_csv"
ACTUAL_DIR="$E2E_DIR/actual/flow"

INSECURE_URL="" SECURE_URL="" TOOL="" ROWS=10000 SEED="$(date +%s)"

usage() {
    die "usage: flow.sh run --insecure-url <url> --secure-url <url> --tool <e2etool> [--rows N] [--seed S]"
}

# Every option is named, because two URLs on a command line need names.
parse_args() {
    [[ "${1:-}" == run ]] || usage
    shift
    while (( $# > 0 )); do
        case "$1" in
            --insecure-url) INSECURE_URL="$2"; shift 2 ;;
            --secure-url)   SECURE_URL="$2"; shift 2 ;;
            --tool)         TOOL="$2"; shift 2 ;;
            --rows)         ROWS="$2"; shift 2 ;;
            --seed)         SEED="$2"; shift 2 ;;
            *)              usage ;;
        esac
    done
    [[ -n "$INSECURE_URL" && -n "$SECURE_URL" && -x "$TOOL" ]] || usage
}

# request <out_dir> <name> <curl args...>: one request, its body in
# <out_dir>/<name>.body, its status on stdout. -g keeps [] literal, and
# --compressed is deliberately not used, because what a plain client
# sends is no Accept-Encoding at all.
request() {
    local out="$1" name="$2"
    shift 2
    curl -sS -g -o "$out/$name.body" -w '%{http_code}' "$@" || die "curl failed: $name"
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
# a non-empty access_token, which is printed.
login() {
    local out="$1" url="$2" body="$3" status token
    status=$(request "$out" login -X POST -H 'Content-Type: application/json' --data-binary "@$body" "$url/api/v2/auth/login")
    expect 200 "$status" "$out" login || return 1
    token=$(jq -r '.access_token // empty' "$out/login.body")
    [[ -n "$token" ]] || { log_error "[FAIL] login: no access_token in body"; return 1; }
    echo "$token"
}

# create_body: the create body of the flow's table, one column of every
# type named v_<type>, the names e2etool's flowSchema spells, sharded by
# a day (shard_size is in milliseconds and required).
create_body() {
    cat <<EOF
{"name":"$TABLE","shard_size":86400000,"columns":[
 {"name":"v_int64","type":"int64"},
 {"name":"v_double","type":"double"},
 {"name":"v_string","type":"string"},
 {"name":"v_symbol","type":"symbol","symtable":"${TABLE}_v_symbol"},
 {"name":"v_blob","type":"blob"},
 {"name":"v_timestamp","type":"timestamp"}]}
EOF
}

# create_table <out_dir> <url> <token>: DELETE then POST /api/v2/tables.
# The table is dropped first so the flow is idempotent, and a 404 on the
# delete is the clean state. The create must answer 201.
create_table() {
    local out="$1" url="$2" token="$3" status
    status=$(request "$out" delete -X DELETE -H "Authorization: Bearer $token" "$url/api/v2/tables/$TABLE")
    [[ "$status" == 204 || "$status" == 404 ]] || { expect 204 "$status" "$out" delete; return 1; }
    status=$(request "$out" create -X POST -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
        --data-binary "$(create_body)" "$url/api/v2/tables")
    expect 201 "$status" "$out" create
}

# ingest <out_dir> <url> <token> <rows>: POST /api/v2/rows as text/csv
# with rows.csv. The answer's rows must equal the count the tool wrote,
# edge rows included, and the tool is the one writer of that number.
ingest() {
    local out="$1" url="$2" token="$3" rows="$4" status got
    status=$(request "$out" ingest -X POST -H "Authorization: Bearer $token" -H 'Content-Type: text/csv' \
        --data-binary "@$out/rows.csv" "$url/api/v2/rows")
    expect 200 "$status" "$out" ingest || return 1
    got=$(jq -r '.rows' "$out/ingest.body")
    [[ "$got" == "$rows" ]] || { log_error "[FAIL] ingest: rows $got, want $rows"; return 1; }
}

# read_back <out_dir> <url> <token>: GET /api/v2/tables/<table>/rows as
# text/csv, with no Accept-Encoding, compared byte for byte with the
# generated file. The reader answers $table and $timestamp first and the
# generated header is the same, so nothing is reordered.
read_back() {
    local out="$1" url="$2" token="$3" status
    status=$(request "$out" read -H "Authorization: Bearer $token" -H 'Accept: text/csv' "$url/api/v2/tables/$TABLE/rows")
    expect 200 "$status" "$out" read || return 1
    if ! cmp -s "$out/rows.csv" "$out/read.body"; then
        log_error "[FAIL] read: CSV differs from rows.csv (generated vs read)"
        diff <(head -c 4096 "$out/rows.csv") <(head -c 4096 "$out/read.body") >&2 || true
        return 1
    fi
}

# run_cluster <name> <url> <login_body_file>: the four steps against one
# server. The seed is in every line, so a failure reproduces with
# --seed.
run_cluster() {
    local name="$1" url="$2" body="$3" token rows
    local out="$ACTUAL_DIR/$name"
    mkdir -p "$out"
    log_info "$name: $url, rows=$ROWS seed=$SEED"
    rows=$("$TOOL" gen --rows "$ROWS" --seed "$SEED" --out "$out" --table "$TABLE") || die "$name: e2etool gen failed"
    token=$(login "$out" "$url" "$body") || return 1
    create_table "$out" "$url" "$token" || return 1
    ingest "$out" "$url" "$token" "$rows" || return 1
    read_back "$out" "$url" "$token" || return 1
    log_info "[OK] $name: $rows rows in as CSV and back as CSV, byte for byte (seed=$SEED)"
}

main() {
    parse_args "$@"
    local failed=0
    mkdir -p "$ACTUAL_DIR"
    printf '{"username":"","secret_key":""}' > "$ACTUAL_DIR/anonymous.json"
    run_cluster insecure "$INSECURE_URL" "$ACTUAL_DIR/anonymous.json" || failed=$((failed + 1))
    run_cluster secure "$SECURE_URL" "$REPO_ROOT/user_private.key" || failed=$((failed + 1))
    (( failed == 0 )) || die "v2 flow: $failed of 2 clusters FAILED (seed=$SEED)"
    log_info "v2 flow: both clusters passed (seed=$SEED)"
}

main "$@"
