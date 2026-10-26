#!/usr/bin/env bash
# The v2 e2e round trip (docs/e2e.md, "The v2 round trip"). It drives the
# built binary over HTTP like a client: it logs in, creates one table per
# body format, ingests the generated rows into each in that format, reads
# and queries them back in every format and coding, and then reads them
# back through Arrow's stock Flight SQL drivers, ADBC and JDBC. Every
# step expects the generated input, so nothing is captured and nothing is
# audited (ADR-0014). Error rows are Go tests and never steps here.
#
#   round-trip.sh run --insecure-http <url> --insecure-flight <url> --secure-http <url> --secure-flight <url> --tool <e2etool> --jdbc-jar <jar> [--insecure-log <file>] [--secure-log <file>] [--rows N] [--seed S]
#
# Each server is named by its cluster, insecure or secure. Each of its
# planes is a URL in the form its client takes: http://host:port for curl
# on the HTTP listener, and grpc+tls://host:port for the ADBC driver on
# the Flight listener, which is TLS only. The JDBC step rewrites the
# Flight URL into the JDBC driver's own form. The HTTPS listener has no
# option, because no step reads it. Each server's log file is optional,
# because a server given as a URL has none, and the warning step is
# skipped for it. The Makefile is the only source of
# URLs, ports and paths, and passes them as options. common.sh supplies
# the helpers and TZ=UTC. Working files go to actual/round-trip/<cluster>/.
# The script needs bash, curl, jq, cmp, gunzip and java.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

require_command curl
require_command jq
require_command cmp
require_command gunzip
require_command java

ACTUAL_DIR="$E2E_DIR/actual/round-trip"

INSECURE_HTTP="" INSECURE_FLIGHT="" SECURE_HTTP="" SECURE_FLIGHT="" TOOL="" JDBC_JAR="" INSECURE_LOG="" SECURE_LOG="" ROWS=10000 SEED="$(date +%s)"

usage() {
    die "usage: round-trip.sh run --insecure-http <url> --insecure-flight <url> --secure-http <url> --secure-flight <url> --tool <e2etool> --jdbc-jar <jar> [--insecure-log <file>] [--secure-log <file>] [--rows N] [--seed S]"
}

# Every option is named, because two URLs on a command line need names.
parse_args() {
    [[ "${1:-}" == run ]] || usage
    shift
    while (( $# > 0 )); do
        case "$1" in
            --insecure-http)   INSECURE_HTTP="$2"; shift 2 ;;
            --insecure-flight) INSECURE_FLIGHT="$2"; shift 2 ;;
            --secure-http)     SECURE_HTTP="$2"; shift 2 ;;
            --secure-flight)   SECURE_FLIGHT="$2"; shift 2 ;;
            --tool)         TOOL="$2"; shift 2 ;;
            --jdbc-jar)     JDBC_JAR="$2"; shift 2 ;;
            --insecure-log) INSECURE_LOG="$2"; shift 2 ;;
            --secure-log)   SECURE_LOG="$2"; shift 2 ;;
            --rows)         ROWS="$2"; shift 2 ;;
            --seed)         SEED="$2"; shift 2 ;;
            *)              usage ;;
        esac
    done
    [[ -n "$INSECURE_HTTP" && -n "$INSECURE_FLIGHT" && -n "$SECURE_HTTP" && -n "$SECURE_FLIGHT" && -x "$TOOL" && -f "$JDBC_JAR" ]] || usage
}

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

# expect_header <out_dir> <name> <header> <want>: the response header
# equals want, or is absent when want is empty, which is how identity
# shows in content-encoding. The match is case-insensitive, because HTTP
# header names are, and the value loses its CR.
expect_header() {
    local out="$1" name="$2" header="$3" want="$4" got
    got=$(grep -i "^$header:" "$out/$name.headers" | head -n 1 | cut -d' ' -f2- | tr -d '\r')
    [[ "$got" == "$want" ]] || { log_error "[FAIL] $name: $header is '$got', want '${want:-absent}'"; return 1; }
}

# compare_csv <out_dir> <name> <format> <table> <body_file> <csv> <csv_is>:
# the body, decoded to CSV through e2etool tocsv, compared byte for byte
# with the generated CSV (ADR-0014 2). A CSV body skips the tool when
# <csv_is> is raw: the reader's CSV is the generated bytes as they are.
# The query's CSV is not, because a bare SELECT * answers $timestamp
# before $table, so the query passes tool and the CSV goes through the
# decoder like every other format. On a mismatch the first bytes of both
# are diffed, because the seed reproduces the rest.
compare_csv() {
    local out="$1" name="$2" format="$3" table="$4" body="$5" csv="$6" csv_is="$7"
    if [[ "$format" != csv || "$csv_is" == tool ]]; then
        "$TOOL" tocsv --format "$format" --table "$table" < "$body" > "$out/$name.csv" \
            || { log_error "[FAIL] $name: e2etool tocsv failed"; return 1; }
        body="$out/$name.csv"
    fi
    if ! cmp -s "$csv" "$body"; then
        log_error "[FAIL] $name: CSV differs from $(basename "$csv") (generated vs read)"
        diff <(head -c 4096 "$csv") <(head -c 4096 "$body") >&2 || true
        return 1
    fi
}

# check_response <out_dir> <name> <format> <media_type> <coding> <table> <csv> <csv_is>:
# the assertions the read, the query and the DuckDB read share once their
# request answered 200. <csv_is> is raw for the read and tool for the
# query and the DuckDB read (compare_csv). content-type must equal the
# format's media type with no charset, as the in-process round trip
# asserts it. content-encoding must equal the coding and be absent for
# identity. No other header is read. A gzip body is decompressed through
# gunzip into <name>.plain first, and then the body is compared with
# the generated CSV.
check_response() {
    local out="$1" name="$2" format="$3" type="$4" coding="$5" table="$6" csv="$7" csv_is="$8" want_coding=""
    local body="$out/$name.body"
    [[ "$coding" == identity ]] || want_coding="$coding"
    expect_header "$out" "$name" content-type "$type" || return 1
    expect_header "$out" "$name" content-encoding "$want_coding" || return 1
    if [[ "$coding" == gzip ]]; then
        gunzip -c "$body" > "$out/$name.plain" || { log_error "[FAIL] $name: body is not gzip"; return 1; }
        body="$out/$name.plain"
    fi
    compare_csv "$out" "$name" "$format" "$table" "$body" "$csv" "$csv_is"
}

# accept_encoding <coding>: the request header of the coding, as curl
# arguments on stdout, one per line. identity sends no Accept-Encoding,
# which is what a plain client sends. gzip sends Accept-Encoding: gzip
# alone, because the first coding the client lists wins.
accept_encoding() {
    [[ "$1" == identity ]] || printf '%s\n%s\n' -H "Accept-Encoding: $1"
}

# read_back <out_dir> <url> <token> <table> <format> <media_type> <coding> <csv>:
# GET /api/v2/tables/<table>/rows under Accept of that type and that
# coding, checked through check_response. The reader answers $table and
# $timestamp first and the generated header is the same, so nothing is
# reordered.
read_back() {
    local out="$1" url="$2" token="$3" table="$4" format="$5" type="$6" coding="$7" csv="$8"
    local name="$table.read.$format.$coding" status
    local -a enc=()
    while IFS= read -r arg; do enc+=("$arg"); done < <(accept_encoding "$coding")
    status=$(request "$out" "$name" -H "Authorization: Bearer $token" -H "Accept: $type" \
        "${enc[@]+"${enc[@]}"}" "$url/api/v2/tables/$table/rows")
    expect 200 "$status" "$out" "$name" || return 1
    check_response "$out" "$name" "$format" "$type" "$coding" "$table" "$csv" raw
}

# query_back <out_dir> <url> <token> <table> <format> <media_type> <coding> <csv>:
# POST /api/v2/query with SELECT * FROM <table> as the text/plain body,
# under Accept of that type and that coding, checked through
# check_response like a read, with every format, CSV included, decoded
# through the tool (compare_csv says why).
query_back() {
    local out="$1" url="$2" token="$3" table="$4" format="$5" type="$6" coding="$7" csv="$8"
    local name="$table.query.$format.$coding" status
    local -a enc=()
    while IFS= read -r arg; do enc+=("$arg"); done < <(accept_encoding "$coding")
    status=$(request "$out" "$name" -X POST -H "Authorization: Bearer $token" -H "Accept: $type" \
        -H 'Content-Type: text/plain' --data-binary "SELECT * FROM $table" \
        "${enc[@]+"${enc[@]}"}" "$url/api/v2/query")
    expect 200 "$status" "$out" "$name" || return 1
    check_response "$out" "$name" "$format" "$type" "$coding" "$table" "$csv" tool
}

# duckdb_back <out_dir> <url> <token> <table> <format> <media_type> <coding> <csv>:
# POST /api/v2/query with #sql SELECT '<table>' AS "$table", * FROM
# qdb.<table> ORDER BY "$timestamp" as the text/plain body, so the select
# runs on DuckDB, checked through check_response like the query, every
# format decoded through the tool. qdb-duck's scan answers no $table
# column, and the tool routes rows by it, so the select names the table
# as a constant.
duckdb_back() {
    local out="$1" url="$2" token="$3" table="$4" format="$5" type="$6" coding="$7" csv="$8"
    local name="$table.duckdb.$format.$coding" status
    local -a enc=()
    while IFS= read -r arg; do enc+=("$arg"); done < <(accept_encoding "$coding")
    status=$(request "$out" "$name" -X POST -H "Authorization: Bearer $token" -H "Accept: $type" \
        -H 'Content-Type: text/plain' \
        --data-binary "#sql SELECT '$table' AS \"\$table\", * FROM qdb.$table ORDER BY \"\$timestamp\"" \
        "${enc[@]+"${enc[@]}"}" "$url/api/v2/query")
    expect 200 "$status" "$out" "$name" || return 1
    check_response "$out" "$name" "$format" "$type" "$coding" "$table" "$csv" tool
}

# TYPES_QUERY is the #sql query of one row of DuckDB-only types (int32,
# boolean, decimal, date), and TYPES_CSV the cells the server's CSV
# encoder writes for it, the ones TestDuckDBScalars pins
# (internal/httpapi/duckdb_test.go), so a driver step proves it carries a
# type QuasarDB does not have. A change to one changes the other. A list
# or struct type was rejected, because the text encoders do not write one.
TYPES_QUERY="#sql SELECT 1 AS i, true AS b, 1.50::DECIMAL(5,2) AS d, DATE '2026-06-11' AS day"
TYPES_CSV=$'i,b,d,day\n1,true,1.50,2026-06-11\n'

# credentials <login_body_file>: the e2etool adbc options of the cluster's
# user, one per line, none for the anonymous user, so the driver
# handshakes on the secure cluster and presents no token on the insecure
# one. The user security file has the login body's shape, so one jq reads
# both.
credentials() {
    local user
    user=$(jq -r '.username' "$1")
    [[ -z "$user" ]] || printf '%s\n%s\n%s\n%s\n' --username "$user" --secret "$(jq -r '.secret_key' "$1")"
}

# adbc_back <out_dir> <flight_url> <login_body_file> <table> <csv>: the
# table over ADBC. e2etool adbc runs SELECT * FROM <table> through arrow-adbc's
# Flight SQL driver and writes Arrow IPC, which compare_csv decodes
# through tocsv and compares with the generated CSV as the query does, so
# the stock driver's result is the rows generated, byte for byte.
adbc_back() {
    local out="$1" flight="$2" body="$3" table="$4" csv="$5"
    local name="$table.adbc"
    local -a creds=()
    while IFS= read -r arg; do creds+=("$arg"); done < <(credentials "$body")
    "$TOOL" adbc --uri "$flight" "${creds[@]+"${creds[@]}"}" --query "SELECT * FROM $table" > "$out/$name.body" \
        || { log_error "[FAIL] $name: e2etool adbc failed"; return 1; }
    compare_csv "$out" "$name" arrow "$table" "$out/$name.body" "$csv" tool
}

# adbc_types <out_dir> <flight_url> <login_body_file>: TYPES_QUERY over ADBC,
# written as CSV by the server's encoder and compared with TYPES_CSV.
adbc_types() {
    local out="$1" flight="$2" body="$3"
    local -a creds=()
    while IFS= read -r arg; do creds+=("$arg"); done < <(credentials "$body")
    printf '%s' "$TYPES_CSV" > "$out/adbc.types.want"
    "$TOOL" adbc --uri "$flight" "${creds[@]+"${creds[@]}"}" --query "$TYPES_QUERY" --format csv > "$out/adbc.types.body" \
        || { log_error "[FAIL] adbc.types: e2etool adbc failed"; return 1; }
    if ! cmp -s "$out/adbc.types.want" "$out/adbc.types.body"; then
        log_error "[FAIL] adbc.types: CSV differs (want vs got)"
        diff "$out/adbc.types.want" "$out/adbc.types.body" >&2 || true
        return 1
    fi
}

# jdbc <out_dir> <name> <flight_url> <login_body_file> <query>: runs the
# query through Arrow's JDBC driver and writes the jar's two lines, the
# column names and the row count, to <out_dir>/<name>.body. The jar takes
# the driver's own URL, jdbc:arrow-flight-sql://host:port. The driver
# needs --add-opens=java.base/java.nio=ALL-UNNAMED on JDK 17 and later.
# The cluster's user logs in on the secure cluster, and the insecure one
# gets no user.
jdbc() {
    local out="$1" name="$2" flight="$3" body="$4" query="$5" user
    local -a creds=()
    user=$(jq -r '.username' "$body")
    [[ -z "$user" ]] || creds=(--user "$user" --password "$(jq -r '.secret_key' "$body")")
    java --add-opens=java.base/java.nio=ALL-UNNAMED -jar "$JDBC_JAR" \
        --url "jdbc:arrow-flight-sql://${flight#grpc+tls://}" "${creds[@]+"${creds[@]}"}" --query "$query" \
        > "$out/$name.body" 2> "$out/$name.log" \
        || { log_error "[FAIL] $name: the JDBC jar failed"; cat "$out/$name.log" >&2; return 1; }
}

# expect_jdbc <out_dir> <name> <names> <rows>: fails unless the jar
# printed these names and this row count.
expect_jdbc() {
    local out="$1" name="$2" names="$3" rows="$4"
    printf '%s\n%s\n' "$names" "$rows" > "$out/$name.want"
    if ! cmp -s "$out/$name.want" "$out/$name.body"; then
        log_error "[FAIL] $name: names or row count differ (want vs got)"
        diff "$out/$name.want" "$out/$name.body" >&2 || true
        return 1
    fi
}

# jdbc_back <out_dir> <flight_url> <login_body_file> <table> <rows>:
# SELECT * FROM the table through the JDBC driver. The expected names come
# from the header of the CSV the ADBC step decoded, which matches the
# generated rows byte for byte. The first two names are swapped, because
# tocsv puts $table in front and a bare SELECT * answers $timestamp first.
# The row count is the number of rows generated.
jdbc_back() {
    local out="$1" flight="$2" body="$3" table="$4" rows="$5" first second rest
    local name="$table.jdbc"
    IFS=, read -r first second rest < "$out/$table.adbc.csv"
    jdbc "$out" "$name" "$flight" "$body" "SELECT * FROM $table" || return 1
    expect_jdbc "$out" "$name" "$second,$first,$rest" "$rows"
}

# jdbc_types <out_dir> <flight_url> <login_body_file>: TYPES_QUERY through
# the JDBC driver, which must answer TYPES_CSV's header and one row. This
# proves the DuckDB describe works for a stock driver.
jdbc_types() {
    local out="$1" flight="$2" body="$3"
    jdbc "$out" jdbc.types "$flight" "$body" "$TYPES_QUERY" || return 1
    expect_jdbc "$out" jdbc.types "${TYPES_CSV%%$'\n'*}" 1
}

# jdbc_runs: whether this platform runs the JDBC steps. FreeBSD does not,
# because the driver's shaded netty allocator fails there at the first
# connection (EmptyByteBuf.memoryAddress throws
# UnsupportedOperationException, build 174), and the owner chose to drop
# the platform from the step (docs/e2e.md, The Flight SQL smoke steps).
jdbc_runs() {
    [[ "$(uname -s)" != FreeBSD ]]
}

# JDBC_OK is the OK line's mention of the JDBC steps, empty where they do
# not run.
if jdbc_runs; then JDBC_OK=" and JDBC"; else JDBC_OK=""; log_info "the JDBC steps do not run on FreeBSD"; fi

# PLAINTEXT_WARNING is the message the server logs at startup when its
# HTTP listener serves a secure cluster, the text newListeners spells
# (cmd/qdb_rest/main.go), so the log step proves the warning fires there
# and nowhere else. A change to one changes the other.
PLAINTEXT_WARNING='http listener on a secure cluster: access tokens travel in plain text'

# check_log <name> <log_file> <present|absent>: the plain-text warning
# is in the server's log, or is not, by a fixed-string grep. An empty
# log file path skips the step with a line saying so, because a server
# given as a URL has no log the harness can read. The step is here and
# not a grep in the Makefile after the driver, so the assertion sits with
# the other steps and the Makefile stays the one source of paths.
check_log() {
    local name="$1" log_file="$2" want="$3"
    # A URL server is a supported way to run the round trip, so a missing
    # log is skipped and not failed.
    if [[ -z "$log_file" ]]; then
        log_info "$name: no log file, the warning step is skipped"
        return 0
    fi
    # A fixed string, because the line is JSON and the message is the one
    # field the driver knows. Matching the JSON by key with jq was
    # rejected, because the driver would then parse a log format it knows
    # nothing else about.
    if grep -qF "$PLAINTEXT_WARNING" "$log_file"; then
        [[ "$want" == present ]] || { log_error "[FAIL] $name: the plain-text warning is in $log_file, want absent"; return 1; }
    else
        [[ "$want" == absent ]] || { log_error "[FAIL] $name: the plain-text warning is not in $log_file, want present"; return 1; }
    fi
    log_info "[OK] $name: the plain-text warning is $want in the log"
}

# The matrix. One table per body format, named after it, and every
# table is read and queried in every response format under every
# coding, and read back through DuckDB SQL the same way. A format is one entry here and a new step is one function
# called in run_cluster.
BODY_FORMATS=("csv:text/csv" "ndjson:application/x-ndjson" "arrow:application/vnd.apache.arrow.stream" "json:application/json")
READ_FORMATS=("csv:text/csv" "ndjson:application/x-ndjson" "arrow:application/vnd.apache.arrow.stream" "json:application/json")
CODINGS=("identity" "gzip")

# run_cluster <name> <http_url> <flight_url> <login_body_file>: the round trip
# against one server: login once, then per body format create the table,
# ingest the generated rows in that format, read, query and read them
# through DuckDB SQL back in every format and coding, and read them back
# through the ADBC and the JDBC driver with the types query after each. The seed is in
# every line, so a failure reproduces with --seed.
run_cluster() {
    local name="$1" url="$2" flight="$3" body="$4" token rows entry fmt table type rentry coding
    local out="$ACTUAL_DIR/$name"
    mkdir -p "$out"
    log_info "$name: $url, rows=$ROWS seed=$SEED"
    token=$(login "$out" "$url" "$body") || return 1
    for entry in "${BODY_FORMATS[@]}"; do
        fmt="${entry%%:*}"; type="${entry#*:}"; table="e2e_$fmt"
        # gen runs once per table, because $table is a column of every
        # body and names the table the rows go to; rewriting it in the
        # driver would format a cell outside the encoders.
        rows=$("$TOOL" gen --rows "$ROWS" --seed "$SEED" --out "$out" --table "$table") || die "$name: e2etool gen failed"
        create_table "$out" "$url" "$token" "$table" || return 1
        ingest "$out" "$url" "$token" "$table" "$type" "$out/rows.$fmt" "$rows" || return 1
        for rentry in "${READ_FORMATS[@]}"; do
            for coding in "${CODINGS[@]}"; do
                read_back "$out" "$url" "$token" "$table" "${rentry%%:*}" "${rentry#*:}" "$coding" "$out/rows.csv" || return 1
                query_back "$out" "$url" "$token" "$table" "${rentry%%:*}" "${rentry#*:}" "$coding" "$out/rows.csv" || return 1
                duckdb_back "$out" "$url" "$token" "$table" "${rentry%%:*}" "${rentry#*:}" "$coding" "$out/rows.csv" || return 1
            done
        done
        adbc_back "$out" "$flight" "$body" "$table" "$out/rows.csv" || return 1
        adbc_types "$out" "$flight" "$body" || return 1
        if jdbc_runs; then
            jdbc_back "$out" "$flight" "$body" "$table" "$rows" || return 1
            jdbc_types "$out" "$flight" "$body" || return 1
        fi
        log_info "[OK] $name: $table, $rows rows in as $fmt, read, queried and read through DuckDB back as ${READ_FORMATS[*]%%:*} under ${CODINGS[*]}, byte for byte, and read back over ADBC${JDBC_OK} (seed=$SEED)"
    done
}

main() {
    parse_args "$@"
    local failed=0
    mkdir -p "$ACTUAL_DIR"
    printf '{"username":"","secret_key":""}' > "$ACTUAL_DIR/anonymous.json"
    run_cluster insecure "$INSECURE_HTTP" "$INSECURE_FLIGHT" "$ACTUAL_DIR/anonymous.json" || failed=$((failed + 1))
    # The insecure server also serves HTTP, so an unconditional warning
    # fails here.
    check_log insecure "$INSECURE_LOG" absent || failed=$((failed + 1))
    run_cluster secure "$SECURE_HTTP" "$SECURE_FLIGHT" "$REPO_ROOT/user_private.key" || failed=$((failed + 1))
    # The secure server serves HTTP on a cluster with a public key, which
    # is the one case the warning is for.
    check_log secure "$SECURE_LOG" present || failed=$((failed + 1))
    (( failed == 0 )) || die "v2 round trip: $failed of 4 checks FAILED, two clusters and two logs (seed=$SEED)"
    log_info "v2 round trip: both clusters passed (seed=$SEED)"
}

main "$@"
