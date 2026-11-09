#!/usr/bin/env bash
# The v1 comparison (docs/e2e.md, The v1 comparison): the old server
# next to the new one, on the secure cluster and then on the insecure
# one. On each cluster it starts both servers, logs in to both, tags
# the table through the new server, sends each query of QUERIES to both
# under each server's token, then the first query again under
# Accept-Encoding gzip, and stops both. The old server is sent
# /api/<path>; the new server /api/v1/<path> and /api/<path>, and each
# of its answers is compared with the old server's. A 200 compares as
# bytes, anything else by status and the shape of the body.
#
#   compare-v1.sh
#
# It takes no options: make test-v1-compare, the only caller, exports
# every path, port and flag it uses, and the names below are the
# Makefile's. The table is the round trip's, which has seeded it on
# both clusters before this runs. No query sends #sql, and none
# exercises a deviation the brief lists, so every difference is a bug.
# common.sh supplies the request helper, the server lifecycle, the old
# server's config and tls pair, and TZ=UTC. Working files go under
# COMPARE_OUT. The script needs bash, curl, jq, cmp and openssl.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

require_command curl
require_command jq
require_command cmp
require_command openssl

# The Makefile's exports, each checked here so a missing one names
# itself instead of failing in a curl line.
: "${OLD_BIN:?}" "${QDB_REST_BIN:?}" "${COMPARE_OUT:?}" "${COMPARE_TABLE:?}"
: "${OLD_PORT:?}" "${OLD_TLS_PORT:?}" "${OLD_PORT_SECURE:?}" "${OLD_TLS_PORT_SECURE:?}"
: "${OLD_PID:?}" "${OLD_LOG:?}" "${OLD_PID_SECURE:?}" "${OLD_LOG_SECURE:?}"
: "${OLD_CONF:?}" "${OLD_CONF_SECURE:?}" "${OLD_TLS_CERT:?}" "${OLD_TLS_KEY:?}"
: "${REST_PORT:?}" "${REST_ARGS:?}" "${REST_PID:?}" "${REST_LOG:?}"
: "${REST_PORT_SECURE:?}" "${REST_ARGS_SECURE:?}" "${REST_PID_SECURE:?}" "${REST_LOG_SECURE:?}"
: "${QDB_URI:?}" "${QDB_URI_SECURE:?}"

OLD_TOKEN="" NEW_TOKEN=""

# TAG is the round trip's tag, which tag_case puts on the table.
TAG=e2e_tag

# QUERIES: the query texts, one per line, with @TABLE@ for the table and
# @TAG@ for the tag. A select star over every type and null, no rows,
# a find that matches, a find that does not, an invalid query, an
# unknown table. No COUNT, because its type word is a listed deviation.
# The range holds no generated row, because the rows start on the last
# day of 2019. The invalid query and the unknown table are the Go
# test's.
QUERIES=(
    "SELECT * FROM @TABLE@"
    "SELECT * FROM @TABLE@ IN RANGE(1970, +1d)"
    "find(tag='@TAG@')"
    "find(tag='@TAG@_none')"
    "NOT A QUERY"
    "SELECT * FROM qdbtest_v1_none"
)

# shape_of <body_file>: the body with every scalar replaced by its JSON
# type and every array cut to its first element, on stdout, keys sorted.
shape_of() {
    jq -S 'walk(if type == "array" then .[:1] elif type == "object" then . else type end)' "$1"
}

# compare <name> <old_status> <old_body> <new_status> <new_body>:
# statuses equal; then bytes equal on 200, shapes equal otherwise.
compare() {
    local name="$1" old_status="$2" old_body="$3" new_status="$4" new_body="$5"
    if [[ "$old_status" != "$new_status" ]]; then
        log_error "[FAIL] $name: status $new_status, the old server answered $old_status"
        head -c 1024 "$new_body" >&2 || true
        echo >&2
        return 1
    fi
    # Bytes on 200 and shape otherwise, because each server words its
    # own error messages.
    if [[ "$old_status" == 200 ]]; then
        if ! cmp -s "$old_body" "$new_body"; then
            # The bodies are one line each, so a diff shows nothing and
            # the first differing bytes say where to look.
            log_error "[FAIL] $name: body differs from the old server's (old vs new, byte, octal old, octal new)"
            cmp -l "$old_body" "$new_body" 2>&1 | head -n 10 >&2 || true
            return 1
        fi
    else
        if ! cmp -s <(shape_of "$old_body") <(shape_of "$new_body"); then
            log_error "[FAIL] $name: $old_status body differs in shape from the old server's"
            { echo "old: $(shape_of "$old_body" | tr -d '\n')"; echo "new: $(shape_of "$new_body" | tr -d '\n')"; } >&2
            return 1
        fi
    fi
    log_info "[OK] $name: $old_status"
}

# login_case <out> <old_url> <new_url> <body_file>: the login on both
# servers, compared by shape and a non-empty token; sets OLD_TOKEN and
# NEW_TOKEN.
login_case() {
    local out="$1" old="$2" new="$3" body="$4" old_status new_status
    # -k, because the secure old server's certificate is self-signed.
    old_status=$(request "$out" login.old -k -X POST -H 'Content-Type: application/json' --data-binary "@$body" "$old/api/login")
    new_status=$(request "$out" login.new -X POST -H 'Content-Type: application/json' --data-binary "@$body" "$new/api/v1/login")
    expect 200 "$old_status" "$out" login.old || return 1
    expect 200 "$new_status" "$out" login.new || return 1
    # The tokens differ by construction, so the shape and not the bytes.
    if ! cmp -s <(shape_of "$out/login.old.body") <(shape_of "$out/login.new.body"); then
        log_error "[FAIL] login: body differs in shape from the old server's"
        { echo "old: $(shape_of "$out/login.old.body" | tr -d '\n')"; echo "new: $(shape_of "$out/login.new.body" | tr -d '\n')"; } >&2
        return 1
    fi
    OLD_TOKEN=$(jq -r '.token // empty' "$out/login.old.body")
    NEW_TOKEN=$(jq -r '.token // empty' "$out/login.new.body")
    [[ -n "$OLD_TOKEN" && -n "$NEW_TOKEN" ]] || { log_error "[FAIL] login: an empty token"; return 1; }
    log_info "[OK] login: 200, the same shape on both servers"
}

# tag_case <out> <new_url>: PUT the tag on the table through the new
# server's v2 route, so the find that matches has a carrier; 204.
tag_case() {
    local out="$1" new="$2" status
    # The round trip detaches the tag at its end, so the comparison
    # attaches it and leaves it: a second PUT is 204 too, and the round
    # trip removes the tag entry before its own tag step.
    status=$(request "$out" tag -X PUT -H "Authorization: Bearer $NEW_TOKEN" "$new/api/v2/tables/$COMPARE_TABLE/tags/$TAG")
    expect 204 "$status" "$out" tag || return 1
    log_info "[OK] tag: $TAG on $COMPARE_TABLE"
}

# query_case <out> <old_url> <new_url> <index> <query>: the query to the
# old server, then to the new server at both spellings, each new answer
# compared with the old.
query_case() {
    local out="$1" old="$2" new="$3" i="$4" query="$5" body old_status status
    body=$(jq -cn --arg q "$query" '{query: $q}')
    # The old server knows the unversioned spelling only.
    old_status=$(request "$out" "q$i.old" -k -X POST -H "Authorization: Bearer $OLD_TOKEN" -H 'Content-Type: application/json' \
        --data-binary "$body" "$old/api/query")
    # The new server answers both spellings, and each is compared with
    # the old.
    status=$(request "$out" "q$i.v1" -X POST -H "Authorization: Bearer $NEW_TOKEN" -H 'Content-Type: application/json' \
        --data-binary "$body" "$new/api/v1/query")
    compare "q$i /api/v1/query $query" "$old_status" "$out/q$i.old.body" "$status" "$out/q$i.v1.body" || return 1
    status=$(request "$out" "q$i.alias" -X POST -H "Authorization: Bearer $NEW_TOKEN" -H 'Content-Type: application/json' \
        --data-binary "$body" "$new/api/query")
    compare "q$i /api/query $query" "$old_status" "$out/q$i.old.body" "$status" "$out/q$i.alias.body" || return 1
}

# labelled <out> <name>: the answer's headers name gzip as its coding;
# the decoded body alone would pass against a plain answer.
labelled() {
    local out="$1" name="$2"
    if ! grep -qi '^content-encoding: *gzip' "$out/$name.headers"; then
        log_error "[FAIL] $name: no Content-Encoding: gzip in the answer"
        return 1
    fi
}

# gzip_case <out> <old_url> <new_url>: the select star under
# Accept-Encoding gzip to the old server, then to the new server at both
# spellings; each answer is labelled gzip, and the decoded bodies are
# compared as bytes through compare.
gzip_case() {
    local out="$1" old="$2" new="$3" body old_status status
    # The select star, because it is the largest body and the one a gzip
    # client sends.
    body=$(jq -cn --arg q "${QUERIES[0]//@TABLE@/$COMPARE_TABLE}" '{query: $q}')
    # --compressed makes curl decode the body, so compare sees plain
    # bytes: the servers compress at different levels, so the wire bytes
    # never match. The header is set by hand, because curl's own header
    # lists every coding it was built with and the case asks for gzip alone.
    old_status=$(request "$out" gz.old -k --compressed -H 'Accept-Encoding: gzip' -X POST -H "Authorization: Bearer $OLD_TOKEN" \
        -H 'Content-Type: application/json' --data-binary "$body" "$old/api/query")
    labelled "$out" gz.old || return 1
    status=$(request "$out" gz.v1 --compressed -H 'Accept-Encoding: gzip' -X POST -H "Authorization: Bearer $NEW_TOKEN" \
        -H 'Content-Type: application/json' --data-binary "$body" "$new/api/v1/query")
    labelled "$out" gz.v1 || return 1
    compare "gzip /api/v1/query" "$old_status" "$out/gz.old.body" "$status" "$out/gz.v1.body" || return 1
    status=$(request "$out" gz.alias --compressed -H 'Accept-Encoding: gzip' -X POST -H "Authorization: Bearer $NEW_TOKEN" \
        -H 'Content-Type: application/json' --data-binary "$body" "$new/api/query")
    labelled "$out" gz.alias || return 1
    compare "gzip /api/query" "$old_status" "$out/gz.old.body" "$status" "$out/gz.alias.body" || return 1
}

# run_cluster <name> <old_url> <new_url> <body_file>: login_case,
# tag_case, query_case over QUERIES, then gzip_case, the bodies under
# COMPARE_OUT/<name>.
run_cluster() {
    local name="$1" old="$2" new="$3" body="$4" out="$COMPARE_OUT/$1" i query
    mkdir -p "$out"
    log_info "$name: old $old, new $new, table $COMPARE_TABLE"
    login_case "$out" "$old" "$new" "$body" || return 1
    tag_case "$out" "$new" || return 1
    for i in "${!QUERIES[@]}"; do
        query="${QUERIES[$i]//@TABLE@/$COMPARE_TABLE}"
        query_case "$out" "$old" "$new" "$i" "${query//@TAG@/$TAG}" || return 1
    done
    gzip_case "$out" "$old" "$new" || return 1
}

# secure_pair: the comparison on the secure cluster, both servers
# started here and stopped at the end.
secure_pair() {
    # The old server takes its cluster key, the harness's user key and
    # the tls pair; the new server takes REST_ARGS_SECURE, as in the
    # round trip. The old server redirects every plain request to its
    # tls port, so it is waited for and dialled there.
    old_config "$OLD_CONF_SECURE" "$QDB_URI_SECURE" "$OLD_PORT_SECURE" "$OLD_TLS_PORT_SECURE" \
        "$REPO_ROOT/cluster_public.key" "$REPO_ROOT/user_private.key" "$OLD_TLS_CERT" "$OLD_TLS_KEY"
    start_server "$OLD_PID_SECURE" "$OLD_LOG_SECURE" "$OLD_TLS_PORT_SECURE" "$OLD_BIN" --config-file "$OLD_CONF_SECURE"
    start_server "$REST_PID_SECURE" "$REST_LOG_SECURE" "$REST_PORT_SECURE" "$QDB_REST_BIN" $REST_ARGS_SECURE
    # The login body is the user security file verbatim, which has the
    # login body's shape.
    run_cluster secure "https://127.0.0.1:$OLD_TLS_PORT_SECURE" "http://127.0.0.1:$REST_PORT_SECURE" \
        "$REPO_ROOT/user_private.key" || return 1
    stop_server "$OLD_PID_SECURE"
    stop_server "$REST_PID_SECURE"
}

# insecure_pair: the comparison on the insecure cluster, both servers
# started here and stopped at the end.
insecure_pair() {
    # A config without keys, so the old server answers on its http port,
    # and the anonymous pair as the login body.
    old_config "$OLD_CONF" "$QDB_URI" "$OLD_PORT" "$OLD_TLS_PORT"
    start_server "$OLD_PID" "$OLD_LOG" "$OLD_PORT" "$OLD_BIN" --config-file "$OLD_CONF"
    start_server "$REST_PID" "$REST_LOG" "$REST_PORT" "$QDB_REST_BIN" $REST_ARGS
    printf '{"username":"","secret_key":""}' > "$COMPARE_OUT/anonymous.json"
    run_cluster insecure "http://127.0.0.1:$OLD_PORT" "http://127.0.0.1:$REST_PORT" "$COMPARE_OUT/anonymous.json" || return 1
    stop_server "$OLD_PID"
    stop_server "$REST_PID"
}

main() {
    # The trap covers a failed step, where a pair is still up. The
    # secure cluster runs first, because the login means something only
    # there. The tls pair is written once, because the old server signs
    # its tokens with the tls key and only the secure one has it.
    trap 'stop_server "$OLD_PID_SECURE"; stop_server "$REST_PID_SECURE"; stop_server "$OLD_PID"; stop_server "$REST_PID"' EXIT
    rm -rf "$COMPARE_OUT"
    mkdir -p "$COMPARE_OUT"
    old_tls_pair "$OLD_TLS_CERT" "$OLD_TLS_KEY"
    secure_pair || die "v1 comparison: secure FAILED"
    insecure_pair || die "v1 comparison: insecure FAILED"
    log_info "v1 comparison: both clusters passed"
}

main "$@"
