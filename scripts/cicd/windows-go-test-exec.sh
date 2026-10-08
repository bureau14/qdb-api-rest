#!/usr/bin/env bash
# This is the go test -exec wrapper for Windows.
#
# It converts PATH to Windows format and execs the generated test binary
# in that environment, so the Windows loader can resolve the MinGW
# runtime and qdb DLLs under the Buildkite/WinSW service context. It
# changes PATH only for the test binary and never for the parent scripts.
# It is copied from qdb-nats-connector.

set -euo pipefail

if (( $# < 1 )); then
    echo "usage: $0 <test-exe> [test-args...]" >&2
    exit 2
fi

exe="$1"
shift

if cygpath_bin="$(command -v cygpath 2>/dev/null)"; then
    path_win="$(${cygpath_bin} -wp "${PATH}")"
    exe="$(${cygpath_bin} -u "${exe}")"

    export PATH="${path_win}"
fi

exec "${exe}" "$@"
