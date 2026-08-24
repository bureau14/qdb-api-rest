#!/usr/bin/env bash
# Buildkite unit-test step for qdb-api-rest.
# Invoked by .buildkite/steps/_build.yml after 20.build.sh.
# Runs the Go test suite with the race detector; output is converted to
# JUnit XML (go-junit-report, installed by cicd_setup_go_toolchain) for
# the qdb-test-report plugin.
#
# The e2e harness (tests/e2e/) is deliberately not run in CI yet (owner
# decision, docs/log.md 2026-08-24); this step is unit tests only.

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

# -mod=vendor: resolve strictly from vendor/; fail loudly instead of fetching.
# -buildvcs=false: same rhel7 uid/no-passwd VCS-stamping failure as 20.build.sh.
GOAMD64="${GOAMD64:-}" \
    "${GO}" test -mod=vendor -buildvcs=false -short -v -race ./... \
    | "${GO_JUNIT_REPORT}" -out "${TEST_REPORT_DIR}/unit-junit-report.xml" -iocopy
