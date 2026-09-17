# Test Strategy -- Plan

Status: draft. This plan re-cuts how the project is tested and measured,
and lists every documentation change that makes the re-cut true. It is a
documentation-only unit of work: no code, script, pipeline or golden
changes here. The harness work it implies is listed under "Follow-up
units" and is planned separately. Conventions: `docs/AGENTS.md`.

## Problems

1. **Three activities are filed under one name.** The documents call
   all of the following "e2e":
   - checking that the server returns the right data (a correctness
     question with a yes/no answer);
   - checking that the new server is a drop-in for the old one (a
     compatibility question, partly yes/no, partly through a real
     client);
   - measuring time to first byte, memory and wall clock (a
     performance question whose answer is a number somebody reads).

   The confusion is concrete. M1's exit asks for "time-to-first-byte
   and server RSS ... recorded in the e2e results" (`docs/log.md`,
   Current state): a benchmark number filed under a harness that has no
   result files. The brief's Testing doctrine item 3 puts performance
   budgets "in the same harness" as the goldens, and `docs/e2e-plan.md`
   lists "stays inside its performance budgets" as a purpose of the
   e2e harness.

2. **The v2 API has no end-to-end test at all.** The harness has one
   suite, the legacy goldens (`make test-legacy`), which stays red
   until M3. Nothing replays a request against `/api/v2/*` of the built
   binary. The Go tests exercise the handlers in-process
   (`internal/httpapi`), which proves the handlers, not the binary:
   flags, config, listeners, middleware order, compression on the wire
   and shutdown are never exercised from the outside.

3. **Nothing end-to-end runs in CI.** The harness was excluded from
   Buildkite on 2026-08-24, and the question of its return was parked
   at M4's entry. Every milestone before M4 therefore closes on local
   evidence only, and the first shippable binary (M3) would ship on a
   compatibility check that no CI run ever made.

4. **"Golden" is defined as "what the old server said", which is not
   quite what we want.** v1 deliberately deviates from the old server in
   a few places (listed under "Deliberate deviations" below). The
   current plan absorbs a deviation as a special case in the replay
   comparator ("the replay treats the two as equal"), which hides the
   deviation in shell code, grows with every deviation, and makes
   "byte-identical" untrue without saying where. v2 has no old server
   to capture from, so the definition does not extend to it at all.

5. **The first performance number arrives late.** Performance is the
   headline requirement, but the bench has no run against the new
   server until the legacy wrappers exist (M3). Removing the TTFB and
   RSS item from M1 without a replacement would push the first number
   even further out.

## The strategy: four layers, one question each

| Layer                      | Question it answers                                                                                      | Mechanism                                                                                            | Runs                                                            | Gate                          |
| -------------------------- | -------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- | ----------------------------- |
| 1. Go tests                | Is the logic right, for any input? Are the four formats equivalent?                                      | `go test`, `rapid` properties against a live qdbd                                                    | Buildkite, all platforms                                        | yes                           |
| 2. e2e goldens             | Does the built binary, driven over HTTP like a client, return exactly the audited response?              | Make + shell + curl: replay a request, byte-compare with the golden; two suites, `v2` and `legacy`   | Buildkite, all platforms                                        | yes                           |
| 3. e2e stress (M4)         | Does the binary behave under load and across a drain? (the session budget bounds load; streams complete) | same harness, pass/fail assertions on behaviour, never on a timing                                   | Buildkite                                                       | yes                           |
| 4. Assessment bench (temp) | How fast is it, how much memory, and does a real legacy client get the same data from old and new?       | `tests/e2e/bench`, Python, result files with fingerprints, wall clock, TTFB, RSS, the two byte flows | developer machine only; needs the old server and qdb-api-python | never; a human reads `report` |

Consequences of the cut:

- Anything whose answer is a number is layer 4. Time to first byte,
  RSS, throughput and wall clock leave the e2e harness, the milestone
  criteria of M1, and the CI-gate language of the brief.
- Anything whose answer is yes or no, observed from outside the
  process, is layer 2 or 3 and runs in Buildkite.
- Drop-in compatibility splits along the same line. **Byte-shape**
  compatibility is a golden replay of the v1 endpoints (layer 2, CI):
  replay needs only the committed goldens, never the old server.
  **Semantic** compatibility at full size through a real client is a
  by-product of the bench run that measures the same pair
  (`legacy@old-rest == legacy@new-rest`, layer 4, local): same code,
  no extra cost, so it stays.
- The old server is needed in exactly two places, both local operator
  steps: capturing legacy goldens, and the bench's `old-rest` runs.

## How the e2e goldens work

### What a golden is

A golden is an **audited expected response**: a run somebody looked at,
judged correct, and committed. Later runs are compared with it byte for
byte. This is qdb-nats-connector's definition (its ADR-007), and it
covers both suites:

- A **v2 golden** is captured from the server under test, audited
  against an independent source (qdbsh or `qdb_export` output for data
  cases; the ADR that owns the wire contract for shape and error
  cases), and committed.
- A **legacy golden** is captured from the old server built from
  `master`; the capture is the audit, because the old server's
  behaviour is the specification. Where v1 deliberately deviates, the
  deviation is an **overlay** (below), never a comparator rule.

No canonicalization and no tolerance: the text encoders are
deterministic (Go's `strconv` renders identically on every platform),
QuasarDB returns rows in a stable order, and an unexpected byte is a bug
worth seeing. Capture never runs in CI; a golden changes only in a
commit whose diff the owner reviews.

### Cases, suites and compare modes

A case is a directory `tests/e2e/golden/<suite>/<NN-slug>/` holding a
hand-written `request.json` and the expected `status`, `headers` and
`body`. Suites:

- `legacy` -- the 18 existing cases, replayed at the unversioned
  spelling and at `/api/v1/<path>` (ADR-0008).
- `v2` -- login, query in each text format, compression, and the rows
  of the ADR-0010 and ADR-0011 error tables that a client can provoke
  from outside (bad bearer, unsupported media type, oversized body,
  invalid query, refused credentials on the secure cluster).

One driver serves both suites. Compare modes: `bytes`; `gunzip`
(decompress, then bytes); `token-shape` (the login answers a token that
differs per call, so the shape is compared: `{"token": ...}` for v1,
RFC 6749's fields for v2). Compared headers are the ones that are part
of a contract: `content-type`, `content-encoding`, and for v2 the
status-bound `www-authenticate`.

Format coverage, decided by what is byte-stable:

- JSON, NDJSON and CSV bodies are golden-compared.
- Arrow IPC is **not** golden-compared: its value slots under nulls come
  from C-allocated buffers handed through zero-copy, so the bytes are
  not guaranteed stable. Arrow's correctness is the format-equivalence
  property test (layer 1); the e2e suite checks status and
  `content-type` only.
- gzip is compared after `gunzip`. zstd is covered by the Go round-trip
  test; it joins e2e only if the `zstd` CLI turns out to be present on
  every agent.

### Deliberate deviations and overlays

v1 is byte-shape identical to the old server except where the project
decided otherwise. The complete list:

| Deviation                                                                  | Decided in                    | Hits a golden         |
| -------------------------------------------------------------------------- | ----------------------------- | --------------------- |
| a `COUNT(...)` column is typed `int64`, where the old server said `count`  | brief, Compatibility contract | yes: 07               |
| readiness failure is `503`, where the old server said `500`                | ADR-0004                      | no (golden 21 is 200) |
| the `"(void)"` / `"(undefined)"` sentinels are not reproduced              | brief, Compatibility contract | no (unreachable)      |
| tokens minted by the old server are rejected                               | brief, Compatibility contract | no                    |
| bad credentials on a secured cluster are `401` at login, not a blind `200` | ADR-0011                      | no (goldens insecure) |
| the dropped endpoints                                                      | brief, "Explicitly dropped"   | no                    |

Mechanism: the captured `body` (and `status`, `headers`) stay exactly
what the old server said, so `make test-legacy-selfcheck` keeps proving
the goldens against the old server. A deviation is a committed overlay
file next to it -- `body.v1`, `status.v1` or `headers.v1` -- written by
hand, which the replay against the server under test prefers over the
captured file. Every deviation is thereby a reviewable diff between two
files in one directory, the full list is `ls golden/legacy/*/*.v1`, and
the comparator stays a plain `cmp`. An overlay exists only for a
deviation listed in the brief's table; the brief is the one home of the
list.

### Dataset

One dataset, the existing one: table `reproduce`, 5,613,032 rows, loaded
by `make load` from the sha256-pinned 64 MB archive. CI loads the same
archive developers do. Data cases bound their own size with `IN RANGE`
or `LIMIT`; the large v2 query case selects a range of roughly 200k rows
so every text encoder crosses its 65536-row chunk boundary several
times. Nothing in the e2e suite selects the whole table: a full-table
response is the bench's business, and "deliberately not captured"
already holds for the legacy suite.

Where an expected body lives: small bodies in git, next to their
`request.json`; a body too large for git (the large query case, per
format) in the dataset archive under `expected/<suite>/<case>/`, which
is where qdb-nats-connector keeps `expected/`. The archive is repackaged
once to add `expected/`; `datasets.json` pins the new sha256.

To verify first in the harness unit (2026-09-17: unmeasured): the
wall-clock time of `make load` on the slowest Buildkite agent. If it is
unacceptable, the fallback is a second, smaller archive of whole shards
loaded under its own table name, with the three `reproduce` legacy
goldens recaptured against it. The fallback is not the plan, because
the full table costs no new tooling and no recapture.

### In Buildkite

A step script `scripts/cicd/40.test-e2e.sh` runs after `30.test.sh` in
every platform's build step, against the qdbd that
`start-services.sh` started and the `bin/qdb_rest` that `20.build.sh`
built: `make -C tests/e2e test-v2` from M1, plus `test-legacy` from M3
(a suite enters CI when it is green, never as a red bar). The script
follows qdb-nats-connector's `50.test-e2e.sh`: GNU make discovery
(`gmake` on FreeBSD, `mingw32-make` on Windows), the Windows DLL
co-location, and a dump of the REST server and qdbd logs on failure.
`curl`, `jq` and `gunzip` are already required by that pipeline on the
same agents.

### The rule for every milestone

An endpoint lands with its goldens. Each milestone's exit criteria name
the suite that must be green in Buildkite on all eight platforms:

- M1: `v2` suite -- login, query in three text formats, gzip, the error
  rows.
- M2: refresh, logout and session cases join `v2`.
- M3: `legacy` suite green at both spellings.
- M4: the stress assertions (layer 3).
- M5: decided at M5's entry; a Flight SQL client does not exist in
  shell, so the suite needs a small Go tool or stays with layer 1.
- M6: exploration cases join `v2`.
- M7: the qdb-nats-connector flow exactly: ingest through the API,
  `qdb_export`, byte-compare with the golden CSV.
- M8: `/api/v2/sql` cases join `v2`.

## How the bench changes

The bench (`docs/bench-plan.md`) stays temporary, local and Python, and
becomes the one home of every measured number, time to first byte and
server RSS included (both are already bench metrics). Two changes:

1. Its plan says so explicitly: TTFB, RSS and throughput are measured
   here and nowhere else; the semantic compatibility check is a
   by-product of the `legacy@new-rest` run.
2. A new registry row, `http-arrow@new-rest`: a protocol module that
   logs in at `/api/v2/auth/login`, posts the query with
   `Accept: application/vnd.apache.arrow.stream` and reads the IPC
   stream with `pyarrow` into DataFrames. It needs nothing beyond M1's
   surface, so the first wall-clock, TTFB and RSS numbers for the
   5.6M-row query exist now instead of at M3. It gates nothing. The
   server under measurement takes `cluster.max_in_buffer_size` and
   `cluster.compression` from the bench Makefile, as the old server
   does.

## How performance budgets change

Performance budgets as CI gates leave the project. Shared CI agents are
too noisy for a time-to-first-byte bound to be anything but flaky or
meaningless, the materialization constraint (brief, Architecture: Data
plane) puts the number mostly outside this binary's control, and the
bench answers the question the budgets were a proxy for. What remains
of "Resilience" in CI is behaviour (layer 3). If a regression guard on
performance is ever wanted, it is a local bench threshold, decided
then.

## Documentation changes

One row per document; every change below is part of this unit.

### `docs/adr/0013-e2e-goldens-in-ci.md` (new, accepted)

Holds the decisions that constrain CI and the harness until reversed:
the four layers and what runs where; the golden definition; overlays for
deliberate deviations; Arrow and zstd outside the golden compare;
performance budgets are not CI gates; a suite enters CI when green.
Rejected alternatives: budgets as CI gates; a comparator tolerance per
deviation; editing captured bodies in place; a sha256 of large bodies
instead of the body (no diff on failure); a canonicalizing comparator;
e2e kept local until M4. Supersedes nothing (the 2026-08-24 exclusion
was a log entry and an `AGENTS.md` fact, never an ADR). Added to
`docs/adr/README.md`.

### `docs/adr/0007-legacy-compatibility-layer.md` (in place)

Two inaccuracies, both predating this unit, and one addition:

- "pinned byte-for-byte by the e2e goldens" ignores the deliberate
  deviations. Becomes: pinned by the e2e goldens, byte for byte except
  the deviations the brief's Compatibility contract lists.
- Three mentions of "sentinel strings" as something the legacy layer
  carries or translates (Context, Decision 2, Consequences). The brief
  does not reproduce the sentinels (owner decision 2026-09-02), so the
  wrapper never writes one. The mentions are removed; the wart list in
  Decision 2 reads: key order, null typing, error bodies, the `find`
  prefix, the header and `?token=` extraction.
- The "red bar" paragraph gains one clause: the suite joins CI when the
  wrappers make it green (ADR-0013).

### Other ADRs

Read and left unchanged, because what they say stays true: ADR-0004
(503 readiness; its deviation is now also listed in the brief's table),
ADR-0008 (goldens keep the unversioned spelling; both-spellings
replay), ADR-0011 ("on an insecure cluster ... the goldens hold"),
ADR-0012 (probe headers are golden-pinned).

### `docs/brief.md`

- **Goals, 1**: "Performance budgets enforced in CI." becomes a
  sentence that performance is measured by the assessment bench and
  correctness is gated in CI.
- **Compatibility contract**: the opening paragraph's "must behave
  byte-shape identically ... Golden responses captured from the old
  server" becomes: byte-shape identical except the deliberate
  deviations; goldens captured from the old server, deviations as
  overlays (ADR-0013). A new subsection "Deliberate deviations" carries
  the table above and becomes its one home; the `COUNT` and sentinel
  bullets under `/api/v1/query`, and the 503 sentence under the probes,
  stay where they are as the specification and the table links them.
- **Testing doctrine**: rewritten to the four layers. Item 2 becomes
  "Golden e2e in Buildkite" (two suites, the golden definition, the
  dataset, every endpoint lands with its goldens); the canonical
  dataset paragraph stays. Item 3 becomes "Stress as behaviour": the
  budgets sentence and "Budgets are versioned numbers in the repo"
  leave; the concurrency and drain assertions stay. Item 4 (the bench)
  gains: the one home of measured numbers; `http-arrow@new-rest` next
  to the other runs.
- **Milestones**: M0's "e2e harness and benchmark scaffolding" stays.
  M1 gains "the v2 golden suite in Buildkite". M3's "with golden
  equivalence tests" becomes "with the legacy golden suite green in
  Buildkite". M4 becomes `/metrics` and the graceful-drain and
  concurrency stress; "performance budgets as gates with their numbers
  versioned in the repo" leaves. M5's bench-retirement sentence stays.
- **Ordering rationale**: the last sentence ("M4's budgets-as-gates
  require the e2e harness in CI ... decision is taken at M4's entry")
  leaves; one sentence replaces it: the e2e goldens run in CI from M1,
  so every milestone closes on CI evidence.
- **Project structure**: the `tests/e2e/` line reads "golden e2e
  harness (make + shell + curl, live qdbd; runs in Buildkite); bench/
  inside is temporary and local".
- **Why this rewrite exists, 1**: unchanged; "the bench measures it" is
  already right.

### `docs/log.md`

- Current state, milestone table: M1's note becomes "query, login and
  compression landed; the v2 golden suite in Buildkite remains"; M3's
  note keeps the red bar and says the suite joins CI when green; M4's
  note ("entry decides whether e2e returns to CI") is emptied.
- M1 criteria: "time-to-first-byte and server RSS ... recorded in the
  e2e results" is replaced by "the v2 golden suite green in Buildkite
  on all eight platforms". M3 criteria: "every legacy golden green
  against `bin/qdb_rest` at both spellings" gains "in Buildkite"; the
  bench fingerprint clause stays (local, owner-run).
- Next, rewritten: (1) the harness unit -- the generalized driver, the
  `v2` suite, overlays, `40.test-e2e.sh`, the load-time check; (2) the
  bench unit -- `http-arrow@new-rest`; (3) the upstream filings,
  unchanged. The old item 3 (M4's CI decision) leaves; its
  `max_in_buffer_size` fact moves to `docs/bench-plan.md`, the only
  place a full-table query still happens.
- Handoff to M3: "Golden 07's `count` column ... the replay treats the
  two as equal" becomes: golden 07 carries a `body.v1` overlay.
- One dated entry: ADR-0013 accepted; e2e goldens run in Buildkite,
  budgets leave the CI gates, TTFB and RSS are the bench's
  (`docs/brief.md`, Testing doctrine). The 2026-08-24 exclusion entry
  stays, as entries are append-only.

### `docs/e2e-plan.md`

- Purpose: three items become two -- serves the audited responses
  (both suites); behaves honestly under stress. The budgets item
  leaves. "It runs on developer machines; whether it returns to
  Buildkite ..." becomes: it runs in Buildkite on every platform and
  on developer machines identically.
- New section "Goldens": the definition, cases and compare modes,
  format coverage, the capture-and-audit workflow per suite, where
  bodies live. "Legacy goldens" becomes a subsection of it and keeps
  its verified facts; the golden-07 bullet becomes the overlay; the
  "Two compatibility layers" paragraph says CI for byte-shape, local
  bench for semantic.
- New section "In Buildkite": the step, the order, the diagnostics, the
  suite-enters-when-green rule.
- Dataset: gains the `expected/` directory in the archive contents and
  the load-time item to verify, with its fallback.
- Stress definition: item 1 (budgets) leaves; item 2 stays as the
  whole definition, stated as behaviour.
- Layout: `test-budgets` and `budgets.env` leave; `test-v2`,
  `capture-v2`, the generalized driver and `golden/v2/` arrive.
- A decision log table dated with this unit: the rows of ADR-0013 that
  are micro-decisions (one driver for two suites; body placement by
  size; full dataset first, slice as fallback).

### `docs/bench-plan.md`

- Purpose: one sentence that the bench is the one home of measured
  numbers, TTFB and RSS included.
- "Protocols, servers, runs": the `http-arrow` protocol row and the
  `http-arrow@new-rest` run row ("the first number for the rewrite;
  needs only the v2 query and login").
- "Server lifecycle": the new server's launch flags that matter for a
  full-table query (`--cluster-max-in-buffer-size`, the value the old
  server takes; `--cluster-compression` from `CAPI_COMPRESSION`), which
  is the fact that leaves the log's Next list.
- "CLI and flow" and "Where the rewrite drops in": the new run listed.

### `AGENTS.md` and README files

An `AGENTS.md` states what is true of its folder, so a fact that
becomes true only with the harness unit is written by that unit, not
here.

- `.buildkite/AGENTS.md`: the fact "The e2e harness ... is not in CI
  (owner decision 2026-08-24). Re-adding it: ..." becomes: the e2e
  harness joins the build step after the Go tests (ADR-0013); until
  its step script exists it is not in CI. The harness unit rewrites the
  fact once more when the step lands, together with the matching
  comment in `.buildkite/steps/_build.yml` and the `40.test-e2e.sh`
  contract bullet in `scripts/cicd/AGENTS.md`.
- `tests/e2e/AGENTS.md`: the goldens bullet covers both suites; the
  rule "captured files are written only by `make capture-golden`" gains
  the overlay rule (a `.v1` file is written by hand, only for a
  deviation the brief lists); capture never runs in CI; no timing
  assertions in this directory.
- `tests/e2e/README.md`: the first line drops "budgets"; usage of the
  new targets is written by the harness unit, when they exist.
- `tests/e2e/bench/AGENTS.md`, `tests/e2e/bench/README.md`: unchanged
  until the bench unit adds the run.
- Root `AGENTS.md`: unchanged; its `tests/e2e/` row names the `v2`
  suite when the harness unit creates it.

### This plan

Deleted when the documentation above has landed; its facts then live in
ADR-0013, the brief, `docs/e2e-plan.md` and `docs/bench-plan.md`. One
log entry says so.

## Follow-up units (not this unit)

1. **Harness**: generalize `legacy.sh` into one golden driver; the `v2`
   suite and its audited goldens; the overlay for golden 07; archive
   repackaged with `expected/`; `scripts/cicd/40.test-e2e.sh` and the
   `_build.yml` line; the load-time measurement. Closes M1.
2. **Bench**: `protocols/http_arrow.py`, `servers/new_rest.py`, the
   registry row; first numbers for the 5.6M-row query.
3. CI evidence for the current tip: the base is ahead of origin, so the
   eight-platform criterion has no run for it; push and trigger through
   the API (`.buildkite/AGENTS.md`).

## Open questions

Each carries the recommendation this plan is written to; approving the
plan settles them.

1. Deviations as overlay files, or as comparator rules? Overlay files.
2. Full dataset in CI, or a slice? Full dataset, slice as the measured
   fallback.
3. Arrow IPC and zstd in the golden compare? No; layer 1 owns both.
4. Performance budgets as CI gates? Leave the project.
5. `http-arrow@new-rest` in the bench now? Yes, as its own unit.
6. A suite in CI while red (the legacy bar before M3)? No; a suite
   enters CI when green.
7. Flight SQL in the e2e suite? Decided at M5's entry.

## Decision log

| Decision                                         | Why                                                                                       | Rejected                                                    |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| Numbers are the bench's, yes/no is CI's          | one question per layer; a number nobody gates on has no place in a gate                   | TTFB and RSS "recorded in the e2e results"                  |
| A golden is an audited response                  | covers v2, which has no old server; makes deviations explicit                             | golden = old-server capture only                            |
| Deviation = overlay file next to the capture     | reviewable diff, plain `cmp`, selfcheck against the old server keeps working              | comparator special cases; hand-editing captured bodies      |
| One dataset, queries bound their own size        | no new packaging tooling, no recapture of goldens 05-07                                   | a CI slice as the first choice                              |
| Large expected bodies in the archive `expected/` | qdb-nats-connector's layout; a failure still diffs against a real body                    | bodies in git; a sha256 in place of the body                |
| e2e in Buildkite from M1                         | every milestone closes on CI evidence; the first shippable binary is CI-proven            | the decision parked at M4's entry                           |
| ADR-0007 corrected in place                      | it contradicts the brief on sentinels and on byte-for-byte; an inaccurate record misleads | a superseding ADR for a correction that changes no decision |

## Commit sequence (the build stage of this unit)

1. `docs(adr): ADR-0013 e2e goldens run in Buildkite; numbers are the bench's`
2. `docs(adr): ADR-0007 names the wrapper's warts as the brief does`
3. `docs(brief): the compatibility contract lists its deliberate deviations`
4. `docs(brief): testing doctrine is four layers, one question each`
5. `docs(brief): milestones exit on goldens in CI; budgets as gates leave`
6. `docs(e2e-plan): goldens are audited responses; two suites, overlays, Buildkite`
7. `docs(bench-plan): measured numbers live here; http-arrow@new-rest`
8. `docs(agents): e2e joins the build step; overlays are hand-written; no timing assertions`
9. `docs(log): M1 exits on the v2 golden suite in Buildkite; ADR-0013 accepted`
10. `docs(log): test-strategy-plan.md deleted; facts moved to ADR-0013, the brief and the two plans`
