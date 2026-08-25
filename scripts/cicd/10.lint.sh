#!/usr/bin/env bash
# Buildkite lint step for qdb-api-rest.
# Invoked by .buildkite/steps/_lint.yml inside bureau14/builder:rhel7.
# Delegates to `make lint` (scripts/cicd/AGENTS.md: the pin has one
# writer, and only this Linux-only step may use make).
#
# The Go toolchain is wired by cicd_setup_go_toolchain (00.common.sh)
# from GOROOT injected by pipeline.py::_go_env_for_agent(); the Makefile
# invokes `go` from PATH, which that function prepends with ${GOROOT}/bin.
# golangci-lint's typecheck compiles the cgo package, so the qdb/ tree
# (qdb-artifacts plugin) and the CGO environment (cicd_setup_qdb_env) are
# needed here exactly as in the build step.

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
