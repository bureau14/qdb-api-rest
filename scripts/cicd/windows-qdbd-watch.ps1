# windows-qdbd-watch.ps1 -- hold a handle on every qdbd.exe (or qdbdd.exe, the
# debug build's name) that is running when the script starts, sample each one's memory, threads and handles once a
# second, and record each one's exit code and exit time. Writes to the file
# named by -Out, one line per event, flushed per line, until every watched
# process has exited or the script is killed. Started by
# cicd_watch_qdbd_start (00.common.sh) on Windows; the test-report plugin
# uploads the file (.buildkite/steps/_build.yml).
#
# The insecure qdbd has died in CI with nothing in its own log, no error dump
# and no Windows fault event (docs/ci-qdbd-logs-plan.md, builds 93 and 97).
# The exit code is the one observation that separates a fault the handlers
# missed (an NTSTATUS such as 0xC0000005), an exit() from inside qdbd (a small
# integer) and a kill from outside (1, or 0xC000013A). Windows keeps the exit
# code only for as long as some process holds a handle on the dead process,
# and nothing in the build step does, so this script holds one from before
# the tests until the trap kills it.

param(
    [Parameter(Mandatory = $true)][string]$Out
)

$ErrorActionPreference = 'Continue'

function Write-Line([string]$line) {
    $stamp = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ss.fffZ')
    Add-Content -Path $Out -Value "$stamp $line"
}

# Get-Process alone opens no handle: the Process object opens one lazily, and
# after the process is gone that open fails, so HasExited turns true with the
# exit code and exit time empty (build 107's haswell sample). Reading Handle
# opens the handle now and the object keeps it, which is what makes the exit
# code readable later. The command line, from WMI, tells the insecure daemon
# (-a 127.0.0.1:2836) from the secure one (2838), so a reader does not need
# the daemon logs for that.
$watched = @(Get-Process -Name qdbd, qdbdd -ErrorAction SilentlyContinue)
if ($watched.Count -eq 0) {
    Write-Line "start: no qdbd.exe or qdbdd.exe is running"
    exit 0
}
foreach ($p in $watched) {
    $null = $p.Handle
    $cmd = (Get-CimInstance Win32_Process -Filter "ProcessId = $($p.Id)" -ErrorAction SilentlyContinue).CommandLine
    Write-Line "start pid=$($p.Id) started=$($p.StartTime.ToUniversalTime().ToString('o')) cmd=$cmd"
}

# One sample per second per live process. A process found exited is reported
# once with its exit code, decimal and hex (an NTSTATUS reads as hex), and
# dropped from the set; the loop ends when the set is empty.
while ($watched.Count -gt 0) {
    Start-Sleep -Seconds 1
    $alive = @()
    foreach ($p in $watched) {
        $p.Refresh()
        if ($p.HasExited) {
            try {
                $code = $p.ExitCode
                $hex = '0x{0:X8}' -f $code
            } catch {
                $code = 'unavailable'
                $hex = $_.Exception.Message
            }
            Write-Line "exit pid=$($p.Id) code=$code hex=$hex exited=$($p.ExitTime.ToUniversalTime().ToString('o'))"
        } else {
            Write-Line "sample pid=$($p.Id) ws=$($p.WorkingSet64) private=$($p.PrivateMemorySize64) threads=$($p.Threads.Count) handles=$($p.HandleCount)"
            $alive += $p
        }
    }
    $watched = $alive
}
Write-Line "end: every watched daemon has exited"
