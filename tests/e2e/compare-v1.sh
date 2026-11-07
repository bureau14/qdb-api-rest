#!/usr/bin/env bash
# The v1 comparison (docs/e2e.md, The v1 comparison): one cluster per
# call. It logs in to the old server and the new one, then sends each
# query of QUERIES to both under each server's token. The old server is
# sent /api/<path>; the new server /api/v1/<path> and /api/<path>, and
# each of its answers is compared with the old server's. A 200 compares
# as bytes, anything else by status and the shape of the body.
#
#   compare-v1.sh --name <cluster> --old <url> --new <url> --table <name> --out <dir> [--login-body <file>]
#
# The Makefile is the only source of URLs and paths, and passes them as
# options. The table is the round trip's, which has seeded it on both
# clusters before this runs. No query sends #sql, and none exercises a
# deviation the brief lists, so every difference is a bug. The login
# body is the user security file on the secure cluster and absent on
# the insecure one, where the anonymous pair is written here. common.sh
# supplies the request helper and TZ=UTC. Working files go to the given
# out directory. The script needs bash, curl, jq and cmp.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

require_command curl
require_command jq
require_command cmp

NAME="" OLD="" NEW="" TABLE="" OUT="" LOGIN_BODY=""
OLD_TOKEN="" NEW_TOKEN=""

# QUERIES: the query texts, one per line, with @TABLE@ for the table.
# A select star over every type and null, no rows, a find that
# matches, a find that does not, an invalid query, an unknown table.
# No COUNT, because its type word is a listed deviation. The range
# holds no generated row, because the rows start on the last day of
# 2019. The invalid query and the unknown table are the Go test's.
QUERIES=(
    "SELECT * FROM @TABLE@"
    "SELECT * FROM @TABLE@ IN RANGE(1970, +1d)"
    "find(tag='e2e_tag')"
    "find(tag='e2e_tag_none')"
    "NOT A QUERY"
    "SELECT * FROM qdbtest_v1_none"
)

usage() {
    die "usage: compare-v1.sh --name <cluster> --old <url> --new <url> --table <name> --out <dir> [--login-body <file>]"
}

parse_args() {
    while (( $# > 0 )); do
        case "$1" in
            --name)       NAME="$2"; shift 2 ;;
            --old)        OLD="$2"; shift 2 ;;
            --new)        NEW="$2"; shift 2 ;;
            --table)      TABLE="$2"; shift 2 ;;
            --out)        OUT="$2"; shift 2 ;;
            --login-body) LOGIN_BODY="$2"; shift 2 ;;
            *)            usage ;;
        esac
    done
    [[ -n "$NAME" && -n "$OLD" && -n "$NEW" && -n "$TABLE" && -n "$OUT" ]] || usage
}

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

# login_case: the login on both servers, compared by shape and a
# non-empty token; sets OLD_TOKEN and NEW_TOKEN.
login_case() {
    local old_status new_status
    old_status=$(request "$OUT" login.old -X POST -H 'Content-Type: application/json' --data-binary "@$LOGIN_BODY" "$OLD/api/login")
    new_status=$(request "$OUT" login.new -X POST -H 'Content-Type: application/json' --data-binary "@$LOGIN_BODY" "$NEW/api/v1/login")
    expect 200 "$old_status" "$OUT" login.old || return 1
    expect 200 "$new_status" "$OUT" login.new || return 1
    # The tokens differ by construction, so the shape and not the bytes.
    if ! cmp -s <(shape_of "$OUT/login.old.body") <(shape_of "$OUT/login.new.body"); then
        log_error "[FAIL] login: body differs in shape from the old server's"
        { echo "old: $(shape_of "$OUT/login.old.body" | tr -d '\n')"; echo "new: $(shape_of "$OUT/login.new.body" | tr -d '\n')"; } >&2
        return 1
    fi
    OLD_TOKEN=$(jq -r '.token // empty' "$OUT/login.old.body")
    NEW_TOKEN=$(jq -r '.token // empty' "$OUT/login.new.body")
    [[ -n "$OLD_TOKEN" && -n "$NEW_TOKEN" ]] || { log_error "[FAIL] login: an empty token"; return 1; }
    log_info "[OK] login: 200, the same shape on both servers"
}

# query_case <index> <query>: the query to the old server, then to the
# new server at both spellings, each new answer compared with the old.
query_case() {
    local i="$1" query="$2" body old_status status
    body=$(jq -cn --arg q "$query" '{query: $q}')
    # The old server knows the unversioned spelling only.
    old_status=$(request "$OUT" "q$i.old" -X POST -H "Authorization: Bearer $OLD_TOKEN" -H 'Content-Type: application/json' \
        --data-binary "$body" "$OLD/api/query")
    # The new server answers both spellings, and each is compared with
    # the old.
    status=$(request "$OUT" "q$i.v1" -X POST -H "Authorization: Bearer $NEW_TOKEN" -H 'Content-Type: application/json' \
        --data-binary "$body" "$NEW/api/v1/query")
    compare "q$i /api/v1/query $query" "$old_status" "$OUT/q$i.old.body" "$status" "$OUT/q$i.v1.body" || return 1
    status=$(request "$OUT" "q$i.alias" -X POST -H "Authorization: Bearer $NEW_TOKEN" -H 'Content-Type: application/json' \
        --data-binary "$body" "$NEW/api/query")
    compare "q$i /api/query $query" "$old_status" "$OUT/q$i.old.body" "$status" "$OUT/q$i.alias.body" || return 1
}

# run_cluster: login_case, then query_case over QUERIES.
run_cluster() {
    local i
    log_info "$NAME: old $OLD, new $NEW, table $TABLE"
    login_case || return 1
    for i in "${!QUERIES[@]}"; do
        query_case "$i" "${QUERIES[$i]//@TABLE@/$TABLE}" || return 1
    done
}

main() {
    parse_args "$@"
    mkdir -p "$OUT"
    if [[ -z "$LOGIN_BODY" ]]; then
        LOGIN_BODY="$OUT/anonymous.json"
        printf '{"username":"","secret_key":""}' > "$LOGIN_BODY"
    fi
    run_cluster || die "v1 comparison: $NAME FAILED"
    log_info "v1 comparison: $NAME passed"
}

main "$@"
