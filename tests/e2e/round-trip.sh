#!/usr/bin/env bash
# The v2 e2e round trip (docs/e2e.md, "The v2 round trip"). It proves the built
# binary over HTTP like a client: login, create a table, ingest the
# generated rows, read them back. The expected value of every step is
# the generated input, so nothing is captured and nothing is audited
# (ADR-0014). Error rows are Go tests, never steps here.
#
#   round-trip.sh run --insecure-url <url> --secure-url <url> --tool <e2etool> [--rows N] [--seed S]
#
# The Makefile is the only source of URLs, ports and paths, and passes
# them as options. common.sh supplies the helpers and TZ=UTC. Working
# files go to actual/round-trip/<cluster>/. The script needs bash, curl, jq
# and cmp.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

require_command curl
require_command jq
require_command cmp

ACTUAL_DIR="$E2E_DIR/actual/round-trip"

INSECURE_URL="" SECURE_URL="" TOOL="" ROWS=10000 SEED="$(date +%s)"

usage() {
    die "usage: round-trip.sh run --insecure-url <url> --secure-url <url> --tool <e2etool> [--rows N] [--seed S]"
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
# <out_dir>/<name>.body, its headers in <out_dir>/<name>.headers, its
# status on stdout. -g keeps [] literal, and --compressed is deliberately
# not used, because what a plain client sends is no Accept-Encoding at
# all. The header dump is what check_response asserts content-type and
# content-encoding from, the way golden.sh captures headers; a -w format
# could not say that a header is absent.
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
# a non-empty access_token, which is printed.
login() {
    local out="$1" url="$2" body="$3" status token
    status=$(request "$out" login -X POST -H 'Content-Type: application/json' --data-binary "@$body" "$url/api/v2/auth/login")
    expect 200 "$status" "$out" login || return 1
    token=$(jq -r '.access_token // empty' "$out/login.body")
    [[ -n "$token" ]] || { log_error "[FAIL] login: no access_token in body"; return 1; }
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

# read_back <out_dir> <url> <token> <table> <media_type> <coding> <csv>:
# GET /api/v2/tables/<table>/rows under Accept of that type, compared
# byte for byte with the generated CSV. The reader answers $table and
# $timestamp first and the generated header is the same, so nothing is
# reordered. identity sends no Accept-Encoding, which is what a plain
# client sends. Only text/csv under identity is implemented; the other
# formats decode through e2etool tocsv and gzip through gunzip when the
# surface definition adds them (docs/log.md, Next).
read_back() {
    local out="$1" url="$2" token="$3" table="$4" type="$5" coding="$6" csv="$7"
    local name="$table.read.${type##*/}.$coding" status
    [[ "$type" == text/csv && "$coding" == identity ]] || die "$name: read format or coding not implemented"
    status=$(request "$out" "$name" -H "Authorization: Bearer $token" -H "Accept: $type" "$url/api/v2/tables/$table/rows")
    expect 200 "$status" "$out" "$name" || return 1
    if ! cmp -s "$csv" "$out/$name.body"; then
        log_error "[FAIL] $name: CSV differs from $(basename "$csv") (generated vs read)"
        diff <(head -c 4096 "$csv") <(head -c 4096 "$out/$name.body") >&2 || true
        return 1
    fi
}

# The matrix. One table per body format, named after it, and every
# table is read in every read format under every coding. A format is
# one entry here and a new step is one function called in run_cluster.
BODY_FORMATS=("csv:text/csv")
READ_FORMATS=("text/csv")
CODINGS=("identity")

# run_cluster <name> <url> <login_body_file>: the round trip against one
# server: login once, then per body format create the table, ingest the
# generated rows in that format and read them back in every format and
# coding. The seed is in every line, so a failure reproduces with --seed.
run_cluster() {
    local name="$1" url="$2" body="$3" token rows entry table type rtype coding
    local out="$ACTUAL_DIR/$name"
    mkdir -p "$out"
    log_info "$name: $url, rows=$ROWS seed=$SEED"
    token=$(login "$out" "$url" "$body") || return 1
    for entry in "${BODY_FORMATS[@]}"; do
        table="e2e_${entry%%:*}"; type="${entry#*:}"
        rows=$("$TOOL" gen --rows "$ROWS" --seed "$SEED" --out "$out" --table "$table") || die "$name: e2etool gen failed"
        create_table "$out" "$url" "$token" "$table" || return 1
        ingest "$out" "$url" "$token" "$table" "$type" "$out/rows.csv" "$rows" || return 1
        for rtype in "${READ_FORMATS[@]}"; do
            for coding in "${CODINGS[@]}"; do
                read_back "$out" "$url" "$token" "$table" "$rtype" "$coding" "$out/rows.csv" || return 1
            done
        done
        log_info "[OK] $name: $table, $rows rows in as $type and back as ${READ_FORMATS[*]} under ${CODINGS[*]}, byte for byte (seed=$SEED)"
    done
}

main() {
    parse_args "$@"
    local failed=0
    mkdir -p "$ACTUAL_DIR"
    printf '{"username":"","secret_key":""}' > "$ACTUAL_DIR/anonymous.json"
    run_cluster insecure "$INSECURE_URL" "$ACTUAL_DIR/anonymous.json" || failed=$((failed + 1))
    run_cluster secure "$SECURE_URL" "$REPO_ROOT/user_private.key" || failed=$((failed + 1))
    (( failed == 0 )) || die "v2 round trip: $failed of 2 clusters FAILED (seed=$SEED)"
    log_info "v2 round trip: both clusters passed (seed=$SEED)"
}

main "$@"
