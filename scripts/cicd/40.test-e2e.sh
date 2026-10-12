#!/usr/bin/env bash
# This is the Buildkite e2e step for qdb-api-rest. .buildkite/steps/_build.yml
# invokes it after 30.test.sh, on every platform. It runs the v2 round
# trip (docs/e2e.md, "In Buildkite") through the harness Makefile,
# against the qdbd start-services.sh started earlier in the step and the
# binary 20.build.sh built.
#
# Five things happen here, in an order that matters:
#
# 1. The preamble of 30.test.sh: trust the workspace, the Go toolchain,
#    the cpu baseline, the qdb/ tree and the cgo environment, because the
#    harness compiles e2etool through cgo and runs cgo binaries.
# 2. On Windows, put the Go directory on PATH in unix form, because the
#    harness Makefile invokes a bare go after sourcing .envrc, and the
#    MSYS shell that mingw32-make spawns cannot parse the Windows-format
#    entry cicd_setup_go_toolchain prepended.
# 3. Find GNU make by its platform name, because the harness Makefile
#    needs GNU make and FreeBSD and Windows do not call it make.
# 4. On Windows, copy the qdb DLLs into bin/, because the service
#    context skips the PATH-based DLL search and both cgo binaries the
#    round trip runs live in bin/.
# 5. Run make test-round-trip with the built binary, and dump the logs
#    when it fails.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(dirname "$(dirname "${SCRIPT_DIR}")")"

source "${SCRIPT_DIR}/00.common.sh"

# 1. The preamble of 30.test.sh, so the harness's go build of e2etool
#    and the cgo binaries it runs find the toolchain and the C API.
cicd_trust_workspace

cd "${BASE_DIR}"

cicd_setup_go_toolchain
cicd_setup_cpu_baseline
cicd_assert_qdb_tree
cicd_setup_qdb_env

# 2. On Windows, the Go directory in unix form. cicd_setup_go_toolchain
#    exports GO as an absolute path and prepends a Windows-style
#    GOROOT\bin to PATH, which the MSYS shell cannot parse (':' is its
#    separator), so the Makefile's bare go would not be found. cygpath
#    exists on MSYS only, so this is a no-op elsewhere.
if command -v cygpath > /dev/null 2>&1; then
    PATH="$(cygpath -u "$(dirname "${GO}")"):${PATH}"
    export PATH
fi

# 3. GNU make. The harness Makefile uses GNU extensions, and the binary's
#    name varies: make on Linux and macOS, gmake on FreeBSD (whose make
#    is BSD make), and mingw32-make on the Windows MSYS2 agents, where
#    make is often absent. The first candidate found wins. This is the
#    one per-platform step that uses make at all (scripts/cicd/AGENTS.md),
#    because the round trip's server lifecycle lives in that Makefile and
#    driving round-trip.sh from here would duplicate its start, trap and
#    stop.
case "$(uname -s)" in
    FreeBSD)              MAKE_CANDIDATES="gmake make" ;;
    MINGW*|MSYS*|CYGWIN*) MAKE_CANDIDATES="make mingw32-make gmake" ;;
    *)                    MAKE_CANDIDATES="make gmake" ;;
esac
MAKE_BIN=""
for _m in ${MAKE_CANDIDATES}; do
    if command -v "${_m}" > /dev/null 2>&1; then
        MAKE_BIN="${_m}"
        break
    fi
done
if [[ -z "${MAKE_BIN}" ]]; then
    echo "ERROR: tests/e2e/Makefile needs GNU make; none of [${MAKE_CANDIDATES}] found on $(uname -s)." >&2
    exit 1
fi
echo "test-e2e: using '${MAKE_BIN}' as GNU make"

# 4. On Windows, the qdb DLLs next to the binaries. The Buildkite agent
#    runs under a service whose spawned processes skip the PATH-based DLL
#    search (NTSTATUS 0xC00000BE), so qdb_rest.exe would exit before
#    main() with no output. Windows always searches the executable's own
#    directory first, and qdb_rest and e2etool are both built into bin/,
#    so one copy serves both.
SUFFIX=""
case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*)
        SUFFIX=".exe"
        mkdir -p "${BASE_DIR}/bin"
        for _dlldir in "${BASE_DIR}/qdb/bin" "${BASE_DIR}/qdb/lib"; do
            [[ -d "${_dlldir}" ]] || continue
            find "${_dlldir}" -maxdepth 1 -name '*.dll' -exec cp -f {} "${BASE_DIR}/bin/" \;
        done
        echo "test-e2e: co-located qdb DLLs into ${BASE_DIR}/bin"
        ls "${BASE_DIR}/bin/"*.dll 2>/dev/null || echo "test-e2e: WARNING no qdb DLLs found to co-locate"
        ;;
esac

# dump_e2e_failure_logs: the tails of the two REST servers' logs and of
# qdbd's console and structured logs for both clusters. The driver
# prints the one assertion that failed, and the server side is never
# surfaced in the build output otherwise, so a refusal or a crash on
# either server would be undiagnosable from the step alone. The REST
# logs are the harness's (tests/e2e/Makefile, REST_LOG and
# REST_LOG_SECURE). The qdbd files sit at the repo root, because
# start-services.sh resolves its paths against the directory it ran
# from (scripts/tests/setup/config.sh). These files are not Buildkite
# artifacts.
dump_e2e_failure_logs() {
    local cluster log_dir

    for cluster in rest rest-secure; do
        echo "+++ e2e failed -- ${cluster}.log"
        local rest_log="${BASE_DIR}/tests/e2e/${cluster}.log"
        [[ -f "${rest_log}" ]] && tail -100 "${rest_log}" || echo "(no log at ${rest_log})"
    done

    for cluster in insecure secure; do
        echo "+++ e2e failed -- qdbd ${cluster} stderr"
        local qdbd_err="${BASE_DIR}/qdbd_log_${cluster}.err.txt"
        [[ -f "${qdbd_err}" ]] && tail -120 "${qdbd_err}" || echo "(no qdbd stderr at ${qdbd_err})"

        echo "+++ e2e failed -- qdbd ${cluster} structured log"
        log_dir="${BASE_DIR}/${cluster}/log"
        if [[ -d "${log_dir}" ]]; then
            find "${log_dir}" -type f -name '*.log' -exec tail -120 {} +
        else
            echo "(no qdbd log dir at ${log_dir})"
        fi
    done
}

# 5. The round trip. The Makefile builds e2etool, starts one server per
#    cluster from QDB_REST_BIN, runs the driver and stops the servers on
#    exit (tests/e2e/Makefile, test-round-trip).
echo "+++ Run the v2 round trip"
if ! "${MAKE_BIN}" -C "${BASE_DIR}/tests/e2e" test-round-trip QDB_REST_BIN="${BASE_DIR}/bin/qdb_rest${SUFFIX}"; then
    dump_e2e_failure_logs
    exit 1
fi
