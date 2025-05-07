#!/usr/bin/env bash

set -eux -o pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/../common.sh"

# Fix permission issue when using docker builds
git config --global --add safe.directory '*'


BUILD_TYPE=${BUILD_TYPE:-Debug}

sed -i -e 's/const GitHash string = .*/const GitHash string = "'${GIT_HASH}'"/' ${BASE_DIR}/meta/version.go
sed -i -e 's/const BuildTime string = .*/const BuildTime string = "'${CURRENT_DATETIME}'"/' ${BASE_DIR}/meta/version.go
sed -i -e 's/const GoVersion string = .*/const GoVersion string = "'${GO_COMPILER_VERSION}'"/' ${BASE_DIR}/meta/version.go
sed -i -e 's/const BuildType string = .*/const BuildType string = "'${BUILD_TYPE}'"/' ${BASE_DIR}/meta/version.go
sed -i -e 's/const Platform string = .*/const Platform string = "'${PLATFORM}'"/' ${BASE_DIR}/meta/version.go

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
