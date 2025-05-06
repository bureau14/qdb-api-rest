#!/usr/bin/env bash

set -eux

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/../common.sh"

#setting version
${SET_VERSION_SCRIPT} 3.15.0

# Fix permission issue when using docker builds
git config --global --add safe.directory '*'

SUFFIX=""

case $(uname) in
    MINGW* )
        SUFFIX=".exe"
        ;;
esac

# Build qdb_rest
(
    pushd ${QDB_REST_DIR}
    ${GO} build -x -v -o qdb_rest$SUFFIX
    popd
)

(
    # Build qdb_rest_service on windows
    case $(uname) in
        MINGW* )
            pushd ${QDB_REST_SERVICE_DIR}
            ${GO} build -x -v -o qdb_rest_service$SUFFIX
            popd
            ;;
    esac
)
