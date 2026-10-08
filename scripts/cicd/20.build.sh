#!/usr/bin/env bash
# This is the Buildkite build step for qdb-api-rest.
# .buildkite/steps/_build.yml invokes it.
# It compiles bin/qdb_rest via ${GO} build directly, because FreeBSD
# ships BSD make and the Windows MSYS2 agents none, composing the same
# -ldflags the root Makefile uses. It then smoke-runs `qdb_rest
# --version`, which prints the injected metadata and the linked C API's
# version, so the smoke run also proves the link (static libqdb_api.a on
# Linux) and, on the other platforms, that the shared library is found at
# load time.
#
# cicd_setup_go_toolchain (00.common.sh) wires the Go toolchain from the
# GOROOT that pipeline.py::_go_env_for_agent() injects. The qdb-artifacts
# plugin populates the qdb/ tree, cicd_assert_qdb_tree checks it is
# there, and cicd_setup_qdb_env (the root .envrc) tells cgo where it is.

set -euxo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(dirname "$(dirname "${SCRIPT_DIR}")")"

source "${SCRIPT_DIR}/00.common.sh"

cicd_trust_workspace

cd "${BASE_DIR}"

cicd_setup_go_toolchain
cicd_setup_cpu_baseline
cicd_assert_qdb_tree
cicd_setup_qdb_env

# --- build ---

# Windows MSYS shells report MINGW* from uname, and the binary gains .exe.
SUFFIX=""
if [[ "$(uname)" == MINGW* ]]; then
    SUFFIX=".exe"
fi

# Same -ldflags as the root Makefile's build target (scripts/cicd/AGENTS.md).
VERSION="$(cat VERSION)"
GIT_SHA="$(git rev-parse HEAD)"
BUILD_TIME="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
BUILD_MODE="release"

# -mod=vendor builds strictly from the committed vendor/ tree and fails
# loudly instead of silently fetching from the proxy if vendor/ is missing
# or stale.
GOFLAGS="-trimpath -mod=vendor"
LDFLAGS="-X main.version=${VERSION} \
         -X main.commit=${GIT_SHA} \
         -X main.buildTime=${BUILD_TIME} \
         -X main.buildMode=${BUILD_MODE} \
         -X main.goamd64=${GOAMD64:-}"

mkdir -p "${BASE_DIR}/bin"

# -buildvcs=false, because Go's auto VCS stamping fails inside
# bureau14/builder:rhel7. The propagated uid has no /etc/passwd entry, so
# git rejects the repo as unsafe in the subprocess go-build spawns. The
# commit SHA is already injected via -X main.commit.
GOFLAGS="${GOFLAGS}" GOAMD64="${GOAMD64:-}" \
    "${GO}" build -buildvcs=false -ldflags "${LDFLAGS}" \
    -o "${BASE_DIR}/bin/qdb_rest${SUFFIX}" ./cmd/qdb_rest

# --- smoke ---

"${BASE_DIR}/bin/qdb_rest${SUFFIX}" --version
