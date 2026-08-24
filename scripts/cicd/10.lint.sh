#!/usr/bin/env bash
# Buildkite lint step for qdb-api-rest.
# Invoked by .buildkite/steps/_lint.yml inside bureau14/builder:rhel7.
# Delegates to the root Makefile's lint target, which owns the
# golangci-lint version pin and installs the linter into bin/ -- one
# writer for the pin, shared between CI and developer machines.  This
# step is Linux-only, so GNU make is available (the per-platform build
# steps avoid make because FreeBSD ships BSD make and the Windows agents
# have none).
#
# The Go toolchain is wired by cicd_setup_go_toolchain (00.common.sh)
# from GOROOT injected by pipeline.py::_go_env_for_agent(); the Makefile
# invokes `go` from PATH, which that function prepends with ${GOROOT}/bin.

set -euxo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(dirname "$(dirname "${SCRIPT_DIR}")")"

source "${SCRIPT_DIR}/00.common.sh"

# Required when the docker plugin propagates the host UID into the container:
# git refuses to operate on a workspace owned by a different user without this.
git config --global --add safe.directory '*'

cd "${BASE_DIR}"

cicd_setup_go_toolchain
cicd_assert_qdb_tree

make lint
