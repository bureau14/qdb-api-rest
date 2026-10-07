#!/usr/bin/env bash
# Shared helpers for the Buildkite CI steps:
#   cicd_setup_go_toolchain -- GOROOT/GOPATH/GO resolution + go-junit-report
#   cicd_setup_cpu_baseline -- QDB_CPU_ARCHITECTURE_CORE2 -> GOAMD64
#   cicd_assert_qdb_tree    -- fail fast when the C API artifact is absent
#   cicd_setup_qdb_env      -- CGO environment, sourced from the root .envrc
#   cicd_trust_workspace    -- let git operate on a checkout owned by another UID
#   cicd_archive_qdbd_logs_on_exit -- EXIT trap: archive both daemons' logs, keep the exit status
#   cicd_record_windows_events -- Windows: the Application and Defender events and whether qdbd is alive, into logs/
#   cicd_watch_qdbd_start   -- Windows: start the watcher that records every qdbd.exe's exit code
#
# Sourced by 10.lint.sh, 20.build.sh and 30.test.sh; not a pipeline
# step (scripts/cicd/AGENTS.md).

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

# cicd_trust_workspace -- the docker plugin propagates the host UID into the
# container, and git refuses to operate on a workspace owned by a different
# user; every step calls this before touching the checkout.
cicd_trust_workspace() {
    git config --global --add safe.directory '*'
}

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
#          GO_JUNIT_REPORT -- converter used by 30.test.sh to turn
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

# cicd_assert_qdb_tree -- fail fast when the extracted C API is absent.
#
# The vendored qdb-api-go compiles through cgo against qdb/ (locations
# from .envrc); without the tree the failure would surface as a cgo
# compiler trace deep inside the build, so this turns it into one clear
# error first.  On Linux the static archive is additionally required: the
# binary links libqdb_api.a statically there, and a c-api package without
# it means a quasardb build that predates QDB-19063.
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

# cicd_setup_qdb_env -- load the CGO environment from the root .envrc.
#
# bash functions share the parent shell's environment, so every `export`
# in .envrc propagates to the calling step script.  Call it after
# cicd_assert_qdb_tree (the paths must exist) and before any ${GO}
# invocation that compiles a package importing internal/qdb -- which
# includes golangci-lint's typecheck and `go test`.  On Windows .envrc
# also prepends the MinGW gcc to PATH, which cgo (and `go test -race`)
# needs to find.
#
# Outputs: CGO_CFLAGS, CGO_LDFLAGS and the per-OS loader path, exported.
cicd_setup_qdb_env() {
    source "${BASE_DIR}/.envrc"
    echo "cicd_setup_qdb_env: CGO_CFLAGS=${CGO_CFLAGS}"
    echo "cicd_setup_qdb_env: CGO_LDFLAGS=${CGO_LDFLAGS}"
    case "$(uname)" in
        FreeBSD) echo "cicd_setup_qdb_env: LD_LIBRARY_PATH=${LD_LIBRARY_PATH}" ;;
        Darwin) echo "cicd_setup_qdb_env: DYLD_LIBRARY_PATH=${DYLD_LIBRARY_PATH}" ;;
        MINGW*) echo "cicd_setup_qdb_env: PATH=${PATH}" ;;
    esac
}

export -f cicd_setup_qdb_env

# cicd_archive_qdbd_logs_on_exit -- EXIT trap for a test step script: archive
# both qdbd daemons' log directories, console files and error dumps into
# logs/, then exit with the status the script was exiting with.
#
# Install it after cd "${BASE_DIR}" with `trap cicd_archive_qdbd_logs_on_exit
# EXIT`. The archives are the submodule's (scripts/tests/setup/cleanup.sh,
# archive), so they have the shape hooks/pre-exit produces, and the test-report
# plugin uploads them from logs/qdbd-logs-*.tar.gz (.buildkite/steps/_build.yml).
# On Windows it also records what the system knows about the daemons
# (cicd_record_windows_events) and stops the watcher cicd_watch_qdbd_start
# started. qdbd keeps running; hooks/pre-exit stops it.
#
# Inputs:  BASE_DIR -- the checkout; cleanup.sh resolves its paths from the cwd.
# Outputs: logs/qdbd-logs-<epoch>-{insecure,secure}.tar.gz, when a log
#          directory exists.
cicd_archive_qdbd_logs_on_exit() {
    # The archive has to happen here, inside the command, and not in a hook:
    # Buildkite runs the repository's post-command and pre-exit hooks after the
    # plugin's post-command hook, which is the upload. The steps:
    #
    #  1. capture the script's exit status before anything else can change it;
    #  2. archive through the submodule in a subshell that cannot fail the
    #     step: a missing tar or log directory is not a test failure;
    #  3. on Windows, record the system's account of the daemons, which is
    #     the only account there is when qdbd dies without logging, and stop
    #     the watcher after it has had time to write a daemon's last state;
    #  4. exit with the captured status so a red test stays red.

    # 1. The first line of the handler reads $? of the exiting command.
    local status=$?
    trap - EXIT

    # 2. cleanup.sh sources config.sh (set -xe, argument parsing) and defines
    # archive; the subshell keeps both out of this shell. qdbd stays up: its
    # log is flushed every QDB_LOG_FLUSH_INTERVAL_MS and on a fatal signal,
    # and stopping would SIGKILL it, which flushes nothing.
    (
        cd "${BASE_DIR}" \
            && source scripts/tests/setup/cleanup.sh \
            && archive
    ) || echo "cicd_archive_qdbd_logs_on_exit: archiving the qdbd logs failed; the step's status is unchanged" >&2

    # 3. The insecure daemon has died in CI with nothing in its own log, its
    # console files or an error dump (docs/ci-qdbd-logs-plan.md, build 93), so
    # the event log is the witness for a death that bypasses its handlers.
    # The watcher samples once a second, so two seconds cover a death right at
    # the end; a daemon still alive would keep it running past the upload,
    # which is why it is killed and not waited for.
    if [[ "$(uname)" == MINGW* ]]; then
        cicd_record_windows_events || echo "cicd_archive_qdbd_logs_on_exit: recording the Windows events failed; the step's status is unchanged" >&2
        if [[ -n "${CICD_QDBD_WATCH_PID:-}" ]]; then
            sleep 2
            kill "${CICD_QDBD_WATCH_PID}" 2> /dev/null || true
        fi
    fi

    # 4. The status of the tests, not of the archive.
    exit "${status}"
}

export -f cicd_archive_qdbd_logs_on_exit

# cicd_record_windows_events -- write logs/windows-events-<epoch>.txt: the
# qdbd.exe (or qdbdd.exe, the debug build) processes alive now, the state of Windows Error Reporting, the
# Application log's crash and error-report events, and the Defender
# operational log's detection events, each from the last two hours. Windows
# only; the test-report plugin uploads the file (.buildkite/steps/_build.yml).
#
# Inputs:  the cwd is the checkout (logs/ is relative to it).
# Outputs: logs/windows-events-<epoch>.txt. A query that fails leaves its
#          error in the file and fails nothing else.
cicd_record_windows_events() {
    # Windows records a process death that bypasses the process's own handlers
    # (a fast-fail, a stack overflow, an allocation failure, a kill from
    # outside) in the Application log: event 1000 carries the exception code
    # and the faulting module, 1001 is the error report, 1026 the .NET variant.
    # Defender's 1116 and 1117 name a file it detected or acted on, and event
    # 1000 is written by the Windows Error Reporting service, so its state
    # (the WerSvc service and the Disabled policy value) says whether an empty
    # Application log means no fault or no reporting. tasklist says whether
    # the daemons are still alive at this point. The queries go
    # through PowerShell, not wevtutil, because MSYS bash rewrites arguments
    # that start with "/" as paths; tasklist's switches are written "//FI", the
    # doubled slash the submodule uses for Taskkill, which MSYS turns into one.
    # Each query is tried on its own, so one that fails (a log the agent user
    # cannot read) leaves the others intact.
    local out="logs/windows-events-$(date +%s).txt"
    mkdir -p logs
    {
        echo "=== tasklist qdbd.exe and qdbdd.exe ($(date -u +%Y-%m-%dT%H:%M:%SZ))"
        tasklist.exe //FI "IMAGENAME eq qdbd.exe" //V 2>&1
        tasklist.exe //FI "IMAGENAME eq qdbdd.exe" //V 2>&1
        echo
        echo "=== Windows Error Reporting: WerSvc and the Disabled policy value"
        powershell.exe -NoProfile -NonInteractive -Command \
            "Get-Service WerSvc -ErrorAction Stop | Format-List Name,Status,StartType | Out-String; Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\Windows Error Reporting' -ErrorAction Stop | Format-List Disabled,DontShowUI | Out-String" 2>&1
        echo
        echo "=== Application log, events 1000 1001 1026, last two hours"
        powershell.exe -NoProfile -NonInteractive -Command \
            "Get-WinEvent -FilterHashtable @{LogName='Application'; Id=1000,1001,1026; StartTime=(Get-Date).AddHours(-2)} -ErrorAction Stop | Format-List TimeCreated,Id,ProviderName,Message | Out-String -Width 4096" 2>&1
        echo
        echo "=== Microsoft-Windows-Windows Defender/Operational, events 1116 1117, last two hours"
        powershell.exe -NoProfile -NonInteractive -Command \
            "Get-WinEvent -FilterHashtable @{LogName='Microsoft-Windows-Windows Defender/Operational'; Id=1116,1117; StartTime=(Get-Date).AddHours(-2)} -ErrorAction Stop | Format-List TimeCreated,Id,Message | Out-String -Width 4096" 2>&1
    } > "${out}"
    echo "cicd_record_windows_events: wrote ${out}"
}

export -f cicd_record_windows_events

# cicd_watch_qdbd_start -- Windows: start windows-qdbd-watch.ps1 in the
# background, which holds a handle on every running qdbd.exe or qdbdd.exe, samples its
# memory, threads and handles once a second and records its exit code and
# exit time in logs/qdbd-watch-<epoch>.txt. A no-op elsewhere. Call it after
# cd "${BASE_DIR}", before the tests; cicd_archive_qdbd_logs_on_exit stops it.
# The reason is in the script's header.
#
# Inputs:  the cwd is the checkout (logs/ is relative to it).
# Outputs: CICD_QDBD_WATCH_PID -- the watcher's pid, for the trap; unset
#          elsewhere.
#          logs/qdbd-watch-<epoch>.txt, written by the watcher.
cicd_watch_qdbd_start() {
    if [[ "$(uname)" != MINGW* ]]; then
        return 0
    fi
    mkdir -p logs
    local out="logs/qdbd-watch-$(date +%s).txt"
    # The script path goes through cygpath because PowerShell is a native
    # program and MSYS converts only arguments it recognizes as paths. The
    # watcher's own output is the file; its stdout is the step log, where a
    # PowerShell error would otherwise vanish.
    powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass \
        -File "$(cygpath -w "${_CICD_SCRIPT_DIR}/windows-qdbd-watch.ps1")" \
        -Out "$(cygpath -w "${out}")" &
    export CICD_QDBD_WATCH_PID=$!
    echo "cicd_watch_qdbd_start: pid ${CICD_QDBD_WATCH_PID} writes ${out}"
}

export -f cicd_watch_qdbd_start
