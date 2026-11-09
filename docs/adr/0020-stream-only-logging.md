# ADR-0020: Logging is one stream, and a Windows service is the package's wrapper

Status: accepted
Date: 2026-11-09

## Context

The brief makes the binary cloud-native: it logs to stdout, and journald
on Linux and the container runtime in Docker and k8s own the capture
(`docs/brief.md`, Vision and Observability and logging). A Windows
service has no stdout, so the brief planned an exception for that one
platform: a self-rotating log file through `natefinch/lumberjack`, the
service lifecycle and fatal events in the Event Log, and an
`observability.log.file` key that exposes the file sink everywhere. The
old Windows package registers `qdb_rest_service.exe`, the old server
linked into a second binary under `kardianos/service`
(`qdb-pkg-windows/qdb-server.iss`, `master:apps/qdb_rest_service/main.go`).

`log/slog` has a logger, a handler and a plain `io.Writer`, and no
rolling file writer. The reference projects split on the question:
Consul, Nomad and Vault carry a rotating writer of their own, Caddy
replaced the unmaintained lumberjack with the fork timberjack in 2025,
and Kubernetes removed file logging and rotation from its components in
1.26 (KEP-2845) and ships `kube-log-runner` for Windows services and
distroless images. The projects that dropped file logging did so for
the Windows-service case, and that is the case the brief's exception
was for.

## Decision

1. The binary writes its log to stdout as one stream. It writes no log
   file, rotates nothing and has no service mode of its own (owner,
   2026-11-09).
2. On Windows the installer runs the binary under a service wrapper that
   captures and rotates stdout and reports the service's lifecycle to
   the Event Log. The wrapper and its configuration live in
   `qdb-pkg-windows` (owner, 2026-11-09).
3. WinSW is the wrapper proposed: MIT, one executable and one XML file,
   stdout rotation modes and Event Log reporting. The package may pick
   an equivalent without a change to this record.

## Consequences

- `internal/observe` keeps one writer, the `io.Writer` that `NewLogger`
  takes, and `main` passes `os.Stdout`. No configuration key names a
  log file.
- `cmd/qdb_rest` gains no `windows/svc` handler and no Event Log
  writer, and the vendor tree gains no module for either.
- `qdb_rest_service.exe` leaves the installer with the SPA assets, and
  the wrapper's registration replaces its `/install` line.
- A start the wrapper cannot complete is the wrapper's to report. The
  binary's own startup failures stay on stderr and exit 1 or 2 as they
  do on every platform.

## Alternatives rejected

| Alternative                                                                   | Why not                                                                                                          |
| ----------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| A `log.file` block with a rotating writer of our own, as Consul carries       | around 150 lines to own for one platform, when a wrapper does the same outside the binary                        |
| A `log.file` block over lumberjack or timberjack                              | a module to carry for the same reason, and lumberjack is unmaintained                                            |
| A native service handler and Event Log writer in the binary, the brief's text | it needs the file sink to be usable at all, and it doubles the lifecycle code the wrapper already has            |
| A second binary linking the server, as `master`'s `qdb_rest_service.exe`      | the server would have to become a library, and the lifecycle code moves instead of disappearing                  |
| A file without rotation, etcd's default                                       | nothing on Windows rotates it, so the file grows until the service restarts                                      |
| NSSM as the wrapper                                                           | an installer-style tool that keeps its configuration in the registry, where the package cannot ship it as a file |
