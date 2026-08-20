#!/usr/bin/env bash
# Legacy golden pairs: capture from the old server, replay against any server.
#
#   legacy.sh capture <base_url> [case ...]   write status/headers/body into golden/legacy/<case>/
#   legacy.sh replay  <base_url> [case ...]   write them to actual/legacy/<case>/ and compare
#
# A case is a directory under golden/legacy/ holding a hand-written
# request.json:
#   method   GET | POST
#   path     e.g. /api/query
#   query    optional, pre-encoded query string (no leading ?)
#   headers  object of request headers
#   body     optional JSON value, sent compact
#   auth     none | bearer | urlparam   (token from an anonymous /api/login,
#            fetched once by the first case that needs one)
#   compare  bytes | gunzip | login-shape
# Captured files: status (3 digits), headers (content-type and
# content-encoding only, lowercased, sorted; absent means absent), body (raw
# bytes; for gunzip, the decompressed bytes). login-shape compares
# {"token": <non-empty string>} instead of bytes, because tokens vary per call.
#
# Pure bash + curl + jq + gunzip. TZ=UTC comes from common.sh.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

MODE="${1:?usage: legacy.sh capture|replay <base_url> [case ...]}"
BASE_URL="${2:?usage: legacy.sh capture|replay <base_url> [case ...]}"
shift 2
GOLDEN_DIR="$E2E_DIR/golden/legacy"
ACTUAL_DIR="$E2E_DIR/actual/legacy"

require_command curl
require_command jq
require_command gunzip
[[ "$MODE" == capture || "$MODE" == replay ]] || die "mode must be capture or replay, got: $MODE"

# Case list: arguments, or every directory with a request.json.
list_cases() {
    if (( $# > 0 )); then printf '%s\n' "$@"; return; fi
    find "$GOLDEN_DIR" -mindepth 2 -maxdepth 2 -name request.json -print | xargs -n1 dirname | xargs -n1 basename | sort
}

# Anonymous login; prints the token, or nothing when the response is not a
# login response (connection refused, 404, non-JSON body).
login() {
    local body
    body=$(curl -sS -X POST -H 'Content-Type: application/json' \
        --data-binary '{"username":"","secret_key":""}' "$BASE_URL/api/login") || return 0
    jq -r '.token // empty' <<<"$body" 2>/dev/null || true
}

# One token per invocation, fetched by the first case whose auth mode needs
# it; a selection of auth-free cases never touches /api/login.
TOKEN=""
ensure_token() {
    [[ -n "$TOKEN" ]] && return
    TOKEN=$(login)
    [[ -n "$TOKEN" ]] || die "login failed against $BASE_URL (is the server up and serving /api/login?)"
}

# Keep only the headers that are part of the compatibility contract.
normalize_headers() {
    tr -d '\r' < "$1" | awk -F': ' 'tolower($1) == "content-type" || tolower($1) == "content-encoding" { print tolower($1) ": " $2 }' | sort
}

# Issue the request of one case; writes status, headers, body into <out_dir>.
# Usage: run_case <case> <out_dir>
run_case() {
    local case="$1" out="$2" req="$GOLDEN_DIR/$1/request.json"
    local method path query auth compare url
    local -a args=()
    method=$(jq -r .method "$req"); path=$(jq -r .path "$req")
    query=$(jq -r '.query // empty' "$req"); auth=$(jq -r .auth "$req"); compare=$(jq -r .compare "$req")
    url="$BASE_URL$path"
    case "$auth" in
        bearer)   ensure_token; args+=(-H "Authorization: Bearer $TOKEN") ;;
        urlparam) ensure_token; query="${query:+$query&}token=$TOKEN" ;;
        none)     ;;
        *)        die "$case: unknown auth mode '$auth'" ;;
    esac
    [[ -n "$query" ]] && url="$url?$query"
    while IFS= read -r h; do args+=(-H "$h"); done < <(jq -r '.headers // {} | to_entries[] | "\(.key): \(.value)"' "$req")
    if jq -e 'has("body")' "$req" >/dev/null; then
        args+=(--data-binary "$(jq -c .body "$req")")
    fi
    mkdir -p "$out"
    # -g: no URL globbing ([] are literal); --compressed is deliberately NOT used.
    curl -sS -g -X "$method" "${args[@]}" -o "$out/body.raw" -D "$out/headers.raw" -w '%{http_code}' "$url" > "$out/status" \
        || die "$case: curl failed against $url"
    echo >> "$out/status"
    normalize_headers "$out/headers.raw" > "$out/headers"
    if [[ "$compare" == gunzip ]]; then
        gunzip -c "$out/body.raw" > "$out/body" || die "$case: response was not gzip"
    else
        mv "$out/body.raw" "$out/body"
    fi
    rm -f "$out/body.raw" "$out/headers.raw"
}

# Compare one replayed case against its golden files; prints the first diff.
# Usage: compare_case <case> -> 0 on match
compare_case() {
    local case="$1" gold="$GOLDEN_DIR/$1" act="$ACTUAL_DIR/$1" compare
    compare=$(jq -r .compare "$gold/request.json")
    [[ -f "$gold/status" ]] || { log_error "[FAIL] $case: no golden captured (run make capture-golden)"; return 1; }
    if ! cmp -s "$gold/status" "$act/status"; then
        log_error "[FAIL] $case: status $(cat "$act/status") != golden $(cat "$gold/status")"; return 1
    fi
    if ! cmp -s "$gold/headers" "$act/headers"; then
        log_error "[FAIL] $case: headers differ"; diff "$gold/headers" "$act/headers" >&2 || true; return 1
    fi
    case "$compare" in
        bytes|gunzip)
            if ! cmp -s "$gold/body" "$act/body"; then
                log_error "[FAIL] $case: body differs (golden vs actual)"
                diff <(head -c 4096 "$gold/body") <(head -c 4096 "$act/body") >&2 || true
                return 1
            fi ;;
        login-shape)
            if ! jq -e 'keys == ["token"] and (.token | type == "string" and length > 0)' "$act/body" >/dev/null 2>&1; then
                log_error "[FAIL] $case: body is not {\"token\": <non-empty string>}"; head -c 512 "$act/body" >&2; echo >&2
                return 1
            fi ;;
        *) die "$case: unknown compare mode '$compare'" ;;
    esac
    log_info "[OK] $case"
}

main() {
    local case failed=0 total=0
    while IFS= read -r case; do
        [[ -f "$GOLDEN_DIR/$case/request.json" ]] || die "unknown case: $case"
        total=$((total + 1))
        if [[ "$MODE" == capture ]]; then
            run_case "$case" "$GOLDEN_DIR/$case"
            log_info "captured $case ($(cat "$GOLDEN_DIR/$case/status"), $(wc -c < "$GOLDEN_DIR/$case/body" | tr -d ' ') bytes)"
        else
            run_case "$case" "$ACTUAL_DIR/$case"
            compare_case "$case" || failed=$((failed + 1))
        fi
    done < <(list_cases "$@")
    if [[ "$MODE" == replay ]]; then
        if (( failed > 0 )); then die "legacy replay: $failed of $total cases FAILED"; fi
        log_info "legacy replay: all $total cases passed"
    fi
}

main "$@"
