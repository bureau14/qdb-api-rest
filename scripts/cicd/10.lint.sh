#!/usr/bin/env bash
# This is the Buildkite lint step for qdb-api-rest.
# .buildkite/steps/_lint.yml invokes it inside bureau14/builder:rhel7.
# It delegates to `make lint`, because the pin has one writer and only
# this Linux-only step may use make (scripts/cicd/AGENTS.md).
#
# cicd_setup_go_toolchain (00.common.sh) wires the Go toolchain from the
# GOROOT that pipeline.py::_go_env_for_agent() injects, and the Makefile
# invokes `go` from PATH, which that function prepends with ${GOROOT}/bin.
# golangci-lint's typecheck compiles the cgo package, so the qdb/ tree
# (qdb-artifacts plugin) and the CGO environment (cicd_setup_qdb_env) are
# needed here as in the build step.

set -euxo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(dirname "$(dirname "${SCRIPT_DIR}")")"

source "${SCRIPT_DIR}/00.common.sh"

cicd_trust_workspace

cd "${BASE_DIR}"

cicd_setup_go_toolchain
cicd_assert_qdb_tree
cicd_setup_qdb_env

make lint
