#!/usr/bin/env bash

set -eu

##
# Define default commands/variables
REALPATH=$(command -v realpath)
SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
BASE_DIR=$(${REALPATH} "${SCRIPT_DIR}/../")
QDB_API_DIR=$(${REALPATH} "${BASE_DIR}/qdb/")
QDB_LIB_DIR=$(${REALPATH} "${QDB_API_DIR}/lib/")

QDB_REST_DIR=$(${REALPATH} "${BASE_DIR}/apps/qdb_rest/")
QDB_REST_SERVICE_DIR=$(${REALPATH} "${BASE_DIR}/apps/qdb_rest_service/")

echo "SCRIPT_DIR: ${SCRIPT_DIR}"
echo "BASE_DIR: ${BASE_DIR}"
echo "QDB_API_DIR: ${QDB_API_DIR}"
echo "QDB_LIB_DIR: ${QDB_LIB_DIR}"
echo "QDB_REST_DIR: ${QDB_REST_DIR}"
echo "QDB_REST_SERVICE_DIR: ${QDB_REST_SERVICE_DIR}"


##
# Validation of the GOROOT and GOPATH env vars

GOROOT=${GOROOT:-}
GOPATH=${GOPATH:-}

if [[ -z "${GOPATH}" ]]
then
    echo "GOPATH environment variable is expect to be set"
    exit 1
fi

GO=""

if [[ -z "${GOROOT}" ]]
then
    echo "GOROOT is not set, using go from path"
    GO=$(command -v go)
else
    echo "GOROOT is set, using go from GOROOT: ${GOROOT}/bin/go"
    GO=$(${REALPATH} "${GOROOT}/bin/go")
fi

if [[ ! -x "${GO}" ]]
then
    echo "Executable not found: ${GO}"
    exit 1
fi

echo "GOROOT: ${GOROOT}"
echo "GOPATH: ${GOPATH}"
echo "GO: ${GO}"

${GO} version

# Propagate .envrc exports to build scripts
source "${BASE_DIR}/.envrc"

export TEST_REPORT_DIR="${BASE_DIR}/test-reports"
mkdir -p "${TEST_REPORT_DIR}"

echo "LD_LIBRARY_PATH=${LD_LIBRARY_PATH}"
echo "CGO_CFLAGS=${CGO_CFLAGS}"
echo "CGO_LDFLAGS=${CGO_LDFLAGS}"

ARCH=""

# Probe architecture, i.e. whether we're amd64 or aarch64

case $(uname) in
    Darwin | Linux | FreeBSD )
        ARCH=$(uname -m)

        # Sanitize architecture description
        if [[ "${ARCH}" == "x86_64" || "${ARCH}" == "amd64" ]]
        then
            ARCH="amd64"
        else
            ARCH="aarch64"
        fi
        ;;

    MINGW* )
        # Don't know how to probe this in windows, but we only do amd64 anyway
        ARCH="amd64"
        ;;

    * )
        echo "Unable to probe environment"
        exit -1
        ;;
esac

OS=""

case $(uname) in
    MINGW* )
        OS="windows"
        ;;

    Darwin )
        OS="darwin"
        ;;

    Linux )
        OS="linux"
        ;;

    FreeBSD )
        OS="freebsd"
        ;;
esac

export PLATFORM="${OS}-${ARCH}"
echo "PLATFORM=${PLATFORM}"

##
# Validate installation of qdb/ base directory
GO=$(${REALPATH} "${GOROOT}/bin/go")

if [[ ! -x "${GO}" ]]
then
    echo "Executable not found: ${GO}"
    exit 1
fi

echo "GOROOT: ${GOROOT}"
echo "GOPATH: ${GOPATH}"
echo "GO: ${GO}"

export GO_COMPILER_VERSION=`${GO} version | cut -d" " -f3`

echo "GO VERSION: ${GO_COMPILER_VERSION}"

export GOROOT="${GOROOT}"
export GOPATH="${GOPATH}"
export GO="${GO}"

export BASE_DIR="${BASE_DIR}"
export QDB_REST_DIR="${QDB_REST_DIR}"
export QDB_REST_SERVICE_DIR="${QDB_REST_SERVICE_DIR}"

export CURRENT_DATETIME=`date +"%Y-%m-%d %H:%M:%S %z"`

git config --global --add safe.directory ${BASE_DIR}
export GIT_HASH=`git rev-parse HEAD`

