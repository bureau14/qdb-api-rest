# ADR-0015: early-preview scope: Flight SQL and DuckDB first, no bench, no dataset, no goldens

Status: accepted
Date: 2026-10-12

## Context

The first deployment of the rewrite is an early preview at a customer
site. That customer needs Arrow Flight SQL and the embedded DuckDB
endpoint and does not use the v1 endpoints or the exploration
endpoints. The milestone order of the brief put drop-in compatibility
and resilience ahead of both, and the test plan around them had grown
past the intent: an assessment bench with its own specification,
five protocol and server pairs and a Python harness, a 5.6M-row
customer-derived dataset loaded in Buildkite on every platform, and a
captured v1 golden suite that depended on that dataset and on a build
of the old server. None of it serves the preview, and the time it
costs is the time the preview does not have.

## Decision

1. **The milestones after M2 are reordered.** M3 is v2 auth, M4 is
   embedded DuckDB, M5 is Flight SQL, M6 is resilience, M7 is
   exploration, M8 is drop-in compatibility and M9 is release. Flight
   SQL follows DuckDB because one Flight SQL endpoint serves both the
   DuckDB query layer and the regular QuasarDB query, so the
   integrated design needs the DuckDB layer to exist first.
2. **The bench is removed.** `tests/e2e/bench/` and `docs/bench.md`
   are deleted. The repository holds no benchmark harness and no
   measured number, and no milestone criterion cites one.
3. **The customer-derived dataset is out of scope.** No test loads
   the `reproduce` table, CI loads no data, and the dataset tooling
   (`make load`, `datasets.json`, the packaging script, the S3
   archive) is deleted. Every e2e suite, the stress included, works
   on rows it generates itself, as the v2 round trip does (ADR-0014).
4. **The v1 golden suite is removed.** The goldens, their driver,
   their fixture and the old-server build are deleted. M8 captures
   what it needs when it starts, and the byte-shape facts of the old
   server are recovered then from the old server's source on
   `master`.

This ADR supersedes ADR-0013. Of its rules, the following stay in
force and are restated here so that nothing cites a superseded
document: the e2e harness asserts behaviour and measures nothing, so
every assertion is pass or fail and no CI gate is a timing or a
number; the e2e harness runs in the Buildkite build step on every
platform and every milestone from M2 on closes on CI evidence of the
built binary; text responses are compared as bytes and Arrow IPC as
decoded content; and the harness builds one Go tool with the server's
toolchain that may import any package of this repository.

## Consequences

- The milestone table in `docs/log.md` and the Milestones section of
  the brief carry the new order. M3 keeps its scope. The criteria of
  each later milestone are written when it starts.
- The brief's Testing doctrine has three layers: Go tests (property
  tests included), the e2e round trip, and the stress. Nothing in the
  repository answers a question whose answer is a number.
- `docs/e2e.md` describes the round trip, the stress definition and
  the service model only. The dataset, the v1 goldens and the old
  server leave it.
- ADR-0014's references to `make verify-dataset`, to the v1 suite and
  the bench as readers of the dataset, and to the bench's
  `http-arrow@new-rest` run name things that no longer exist. Its
  decision stands as written.
- ADR-0007 and ADR-0008 still describe the v1 layer. They apply when
  M8 starts, and the golden suite they mention is the one M8 builds.
- The Handoff to M4 in `docs/log.md` and the M4 criteria are deleted.
  The v1 byte-shape facts they pointed to leave the repository with
  `docs/e2e.md`'s goldens section, and M8 derives them again from
  `master`.

## Alternatives rejected

| Alternative                                             | Why not                                                                                                              |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| Keep the bench and only drop it from milestone criteria | the owner judged it over-engineered and irrelevant to the preview, and a kept tool keeps asking for time             |
| Keep the twelve goldens that do not read the dataset    | the suite still needs the old-server build, and M8 can recapture in less time than keeping them green                |
| A smaller dataset archive for CI                        | the preview needs no customer-derived data at all, and the round trip already proves the binary on rows it generates |
| Flight SQL before DuckDB                                | the Flight SQL endpoint would be designed twice, once without the DuckDB layer and once with it                      |
