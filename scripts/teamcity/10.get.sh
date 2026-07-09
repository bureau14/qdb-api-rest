#!/usr/bin/env bash

set -eux

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/../common.sh"

pushd ${QDB_REST_DIR}
# Dependencies are committed in vendor/ (including qdb-api-go), so CI should
# fail loudly if the vendor tree is missing or stale instead of fetching from
# the network or relying on a sibling ../qdb-api-go checkout.
# -buildvcs=false avoids Go 1.18+ VCS stamping failures in dockerized
# Buildkite checkouts where git rejects the propagated uid as unsafe.
${GO} list -mod=vendor -buildvcs=false ./...

popd
