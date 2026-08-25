#!/usr/bin/env bash
# Buildkite unit-test step for qdb-api-rest.
# Invoked by .buildkite/steps/_build.yml after 20.build.sh.
# Runs the Go test suite with the race detector; output is converted to
# JUnit XML (go-junit-report, installed by cicd_setup_go_toolchain) for
# the qdb-test-report plugin.
#
# The e2e harness (tests/e2e/) is deliberately not run in CI (owner
# decision, .buildkite/AGENTS.md); this step is unit tests only.

set -euxo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(dirname "$(dirname "${SCRIPT_DIR}")")"

source "${SCRIPT_DIR}/00.common.sh"

# Required when the docker plugin propagates the host UID into the container:
# git refuses to operate on a workspace owned by a different user without this.
git config --global --add safe.directory '*'

cd "${BASE_DIR}"

cicd_setup_go_toolchain
cicd_setup_cpu_baseline
cicd_setup_c_toolchain

# On Windows the generated test binaries run through the -exec wrapper,
# which converts PATH to Windows format so the loader resolves the MinGW
# runtime DLLs under the Buildkite service context (see the wrapper).
GO_EXTRA_FLAGS=()
if [[ "$(uname)" == MINGW* ]]; then
    GO_EXTRA_FLAGS+=(-exec "bash ${SCRIPT_DIR}/windows-go-test-exec.sh")
fi

# -mod=vendor: resolve strictly from vendor/; fail loudly instead of fetching.
# -buildvcs=false: same rhel7 uid/no-passwd VCS-stamping failure as 20.build.sh.
GOAMD64="${GOAMD64:-}" \
    "${GO}" test "${GO_EXTRA_FLAGS[@]+"${GO_EXTRA_FLAGS[@]}"}" -mod=vendor -buildvcs=false -short -v -race ./... \
    | "${GO_JUNIT_REPORT}" -out "${TEST_REPORT_DIR}/unit-junit-report.xml" -iocopy
