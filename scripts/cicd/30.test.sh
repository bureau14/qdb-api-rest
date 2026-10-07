#!/usr/bin/env bash
# Buildkite test step for qdb-api-rest: `go test ./...` against the qdbd
# that start-services.sh started earlier in the _build.yml chain, nothing
# skipped. Output is converted to JUnit XML (go-junit-report, installed by
# cicd_setup_go_toolchain) for the qdb-test-report plugin.
# At exit, whatever the tests' outcome, both qdbd daemons' logs are archived
# into logs/ for the test-report plugin to upload (cicd_archive_qdbd_logs_on_exit).
# On Windows a watcher records every qdbd.exe's exit code for the same upload
# (cicd_watch_qdbd_start).

set -euxo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(dirname "$(dirname "${SCRIPT_DIR}")")"

source "${SCRIPT_DIR}/00.common.sh"

cicd_trust_workspace

cd "${BASE_DIR}"

# The archive runs inside this command because the plugin uploads before any
# repository hook runs (00.common.sh, cicd_archive_qdbd_logs_on_exit).
trap cicd_archive_qdbd_logs_on_exit EXIT

cicd_setup_go_toolchain
cicd_setup_cpu_baseline
cicd_assert_qdb_tree
cicd_setup_qdb_env

# The watcher takes its handles before the first request, so a death at any
# point of the run has its exit code recorded (windows-qdbd-watch.ps1).
cicd_watch_qdbd_start

# On Windows the generated test binaries run through the -exec wrapper,
# which converts PATH to Windows format so the loader resolves the MinGW
# runtime DLLs under the Buildkite service context (see the wrapper).
GO_EXTRA_FLAGS=()
if [[ "$(uname)" == MINGW* ]]; then
    GO_EXTRA_FLAGS+=(-exec "bash ${SCRIPT_DIR}/windows-go-test-exec.sh")
fi

# -mod=vendor: resolve strictly from vendor/; fail loudly instead of fetching.
# -buildvcs=false: same rhel7 uid/no-passwd VCS-stamping failure as 20.build.sh.
# No -short: every test assumes qdbd is up (started in the build step).
GOAMD64="${GOAMD64:-}" \
    "${GO}" test "${GO_EXTRA_FLAGS[@]+"${GO_EXTRA_FLAGS[@]}"}" -mod=vendor -buildvcs=false -v -race ./... \
    | "${GO_JUNIT_REPORT}" -out "${TEST_REPORT_DIR}/unit-junit-report.xml" -iocopy
