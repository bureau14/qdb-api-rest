#!/usr/bin/env bash

set -eux

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/../common.sh"

pushd ${QDB_REST_DIR}
${GO} list -mod=vendor -buildvcs=false ./...

popd
