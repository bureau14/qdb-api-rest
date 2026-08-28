# ADR-0002: Logging: the logger travels in context

Status: accepted
Date: 2026-08-23
Milestone: M0

## Context

The brief fixes `slog` as the only logging API, forbids package-level
mutable state, and requires `context.Context` through every call path.
Log lines are only useful in production when every line of one request
carries the same identifying attributes (request id; later user and
session) without each call site repeating them. Go has no dynamic scope
and no goroutine-local storage, so "the current scope's attributes" need
an explicit carrier.

## Decision

- The process logger is placed in the root `context.Context` by `main`
  (`observe.WithLogger`) and reached everywhere through
  `observe.Logger(ctx)`. `slog.SetDefault` is never called; a context
  without a logger is a programming error and `Logger` panics on it --
  fail fast, the project norm, and it keeps every caller free of
  fallback logic.
- Scope is expressed as a child context: `observe.WithAttrs(ctx, ...)`
  returns a ctx whose logger carries the attributes on every record.
  The caller keeps its own ctx, so attributes end with the child's
  lexical scope; a goroutine handed the child ctx inherits them.
- Call sites use the `*Context` methods (`InfoContext(ctx, ...)`), so a
  handler that reads trace state from ctx can be swapped in without
  touching any call site.
- Enrichment happens once per request at the edge: the HTTP middleware
  tags the ctx with `request_id` (inbound `X-Request-Id` when well-formed,
  minted otherwise, always echoed) and writes one access line; auth adds
  user and session the same way; handlers never re-tag.
- Shared attribute names and `observe.Err` live in `internal/observe`;
  any type holding a secret implements `slog.LogValuer`.

## Consequences

- Every function already takes ctx, so the logger costs no signatures.
- Lines of one request join on `request_id`; method, path, status and
  duration appear once, on the access line.
- Trace correlation (OpenTelemetry reads spans from the same ctx) is a
  handler change inside `observe`.
- Tests inject a logger by building a ctx; nothing global to reset.

## Alternatives rejected

| Alternative                            | Why not                                                                                  |
| -------------------------------------- | ---------------------------------------------------------------------------------------- |
| `slog.SetDefault` + bare `slog.Info`   | hidden global state; no per-request scope; tests share one writer                        |
| Goroutine-local "dynamic scope" libs   | no stdlib support, runtime tricks, and scope does not follow work across goroutines      |
| `*slog.Logger` as an explicit argument | doubles every signature to carry what ctx already carries                                |
| Method/path on every line              | noise on streaming paths that log per batch; the access line plus the id already join it |
