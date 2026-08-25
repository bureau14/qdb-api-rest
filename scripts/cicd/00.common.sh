#!/usr/bin/env bash
# Shared helpers for the Buildkite CI steps:
#   cicd_setup_go_toolchain -- GOROOT/GOPATH/GO resolution + go-junit-report
#   cicd_setup_cpu_baseline -- QDB_CPU_ARCHITECTURE_CORE2 -> GOAMD64
#   cicd_assert_qdb_tree    -- fail fast when the C API artifact is absent
#
# Sourced by 10.lint.sh, 20.build.sh and 30.test-unit.sh; not an
# executable pipeline step (the leading 00. signals "loaded first, runs
# nothing").  There is no CGO env helper yet: nothing in this repo links
# the C API until M1 vendors qdb-api-go; when that lands, the canonical
# CGO environment moves here (qdb-nats-connector's .envrc pattern is the
# reference).

set -eu

# Resolve repo root (two levels up: scripts/cicd/ -> scripts/ -> root).
if command -v realpath > /dev/null 2>&1; then
    _CICD_SCRIPT_DIR="$(realpath "$(dirname "${BASH_SOURCE[0]}")")"
else
    _CICD_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
BASE_DIR="$(dirname "$(dirname "${_CICD_SCRIPT_DIR}")")"
export BASE_DIR
export TEST_REPORT_DIR="${BASE_DIR}/test-reports"
mkdir -p "${TEST_REPORT_DIR}"

# cicd_setup_go_toolchain -- derive GO from GOROOT and validate the binary.
#
# Inputs:  GOROOT  -- set by .buildkite/pipeline.py::_go_env_for_agent() from
#                     the per-OS QDB_CICD_AGENT_GO<slug>_ROOT agent env var;
#                     the Buildkite agent shell substitutes the value at
#                     job-start.
#          GOPATH  -- set by the same mechanism from QDB_CICD_AGENT_GO<slug>_PATH.
#
# Outputs: GO      -- absolute path to the go binary (${GOROOT}/bin/go[.exe]).
#          GOROOT, GOPATH, PATH -- re-exported (PATH prepended with ${GOROOT}/bin).
#          GO_JUNIT_REPORT -- converter used by 30.test-unit.sh to turn
#                     `go test` output into the JUnit XML Buildkite reports on.
cicd_setup_go_toolchain() {
    if [[ -z "${GOROOT:-}" ]]; then
        echo "cicd_setup_go_toolchain: GOROOT is not set." >&2
        echo "Expected injection from pipeline.py::_go_env_for_agent() via QDB_CICD_AGENT_GO<slug>_ROOT." >&2
        return 1
    fi

    # Windows MSYS shells report MINGW* from uname; the go binary uses .exe there.
    local suffix=""
    if [[ "$(uname)" == MINGW* ]]; then
        suffix=".exe"
    fi

    GO="${GOROOT}/bin/go${suffix}"

    if [[ ! -x "${GO}" ]]; then
        echo "cicd_setup_go_toolchain: go binary not executable at ${GO}" >&2
        echo "cicd_setup_go_toolchain: GOROOT=${GOROOT}" >&2
        echo "cicd_setup_go_toolchain: contents of ${GOROOT}/bin:" >&2
        ls "${GOROOT}/bin" >&2 || true
        return 1
    fi

    export GO GOROOT GOPATH="${GOPATH:-}"
    PATH="${GOROOT}/bin:${PATH}"
    export PATH

    if ! command -v go-junit-report > /dev/null 2>&1; then
        echo "go-junit-report not found, installing"
        "${GO}" install github.com/jstemmer/go-junit-report/v2@latest
    fi
    export GO_JUNIT_REPORT="${GOPATH}/bin/go-junit-report"
    "${GO_JUNIT_REPORT}" --version

    echo "cicd_setup_go_toolchain: GOROOT=${GOROOT}"
    echo "cicd_setup_go_toolchain: GOPATH=${GOPATH:-}"
    echo "cicd_setup_go_toolchain: GO=${GO}"
    echo "cicd_setup_go_toolchain: $("${GO}" version)"
}

export -f cicd_setup_go_toolchain

# cicd_setup_cpu_baseline -- translate QDB_CPU_ARCHITECTURE_CORE2 into GOAMD64.
#
# QDB_CPU_ARCHITECTURE_CORE2 is quasardb's canonical "build for the legacy
# baseline" switch, set per-platform by .buildkite/pipeline.py exactly as
# quasardb's own pipeline does.  core2 compiles as -march=core2 (up to SSSE3
# only); GOAMD64=v2 requires SSE4.2 and POPCNT, which Core 2 does not have,
# so v1 is the correct floor.  The default build is -march=haswell == v3.
# There is deliberately no positive haswell flag on either side -- absence
# means haswell.
#
# Inputs:  GO -- resolved by cicd_setup_go_toolchain; call that first.
#          QDB_CPU_ARCHITECTURE_CORE2 -- "ON", or absent on haswell/ARM legs.
#
# Outputs: GOAMD64 -- exported; go build and go test read it from the env.
cicd_setup_cpu_baseline() {
    local goarch
    goarch="$("${GO}" env GOARCH | tr -d '\r')"

    if [[ "${goarch}" != "amd64" ]]; then
        unset GOAMD64
        echo "cicd_setup_cpu_baseline: GOARCH=${goarch}, GOAMD64 not applicable"
        return
    fi

    if [[ "${QDB_CPU_ARCHITECTURE_CORE2:-OFF}" == "ON" ]]; then
        export GOAMD64="v1"
    else
        export GOAMD64="v3"
    fi

    echo "cicd_setup_cpu_baseline: QDB_CPU_ARCHITECTURE_CORE2=${QDB_CPU_ARCHITECTURE_CORE2:-OFF} GOAMD64=${GOAMD64}"
}

export -f cicd_setup_cpu_baseline

# cicd_setup_c_toolchain -- make the platform C compiler visible to go.
#
# `go test -race` needs cgo, and cgo needs a C compiler go can find. On
# the Buildkite Windows agents gcc.exe lives in native C:\mingw64\bin,
# which is /c/mingw64/bin under MSYS -- the MSYS-internal /mingw64/bin
# does NOT resolve there, and without this prepend go.exe finds no gcc
# and silently disables cgo ("go: -race requires cgo"). Reference:
# qdb-nats-connector's .envrc, MINGW branch. On Linux, CC/CXX arrive as
# gcc15 paths via pipeline env; FreeBSD and macOS find cc natively, so
# only Windows needs help.
cicd_setup_c_toolchain() {
    if [[ "$(uname)" == MINGW* ]]; then
        export PATH="/c/mingw64/bin:${PATH}"
        echo "cicd_setup_c_toolchain: prepended /c/mingw64/bin to PATH"
    fi
}

export -f cicd_setup_c_toolchain

# cicd_assert_qdb_tree -- fail fast when the extracted C API is absent.
#
# Nothing in this repo links the C API yet (that starts when M1 vendors
# qdb-api-go); the assertion exists so the qdb-artifacts download -- the
# artifact dance every later milestone builds on -- is proven on every
# platform now, while the build is trivial and failures are cheap to debug.
# On Linux the static archive is additionally required: the rewrite links
# libqdb_api.a statically there, and a c-api package without it means a
# quasardb build that predates QDB-19063.
cicd_assert_qdb_tree() {
    if [[ ! -d "${BASE_DIR}/qdb/lib" || ! -d "${BASE_DIR}/qdb/include" ]]; then
        echo "ERROR: expected qdb/lib and qdb/include to be present." >&2
        echo "The qdb-artifacts plugin download (declared in .buildkite/steps/) populates qdb/." >&2
        return 1
    fi
    if [[ "$(uname)" == "Linux" && ! -f "${BASE_DIR}/qdb/lib/libqdb_api.a" ]]; then
        echo "ERROR: expected qdb/lib/libqdb_api.a for the static Linux link." >&2
        return 1
    fi
    echo "cicd_assert_qdb_tree: qdb/ layout ok"
}

export -f cicd_assert_qdb_tree
