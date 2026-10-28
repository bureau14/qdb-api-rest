# Project Log

Status: living document. The top block is rewritten in place. The entries
below are append-only, newest first. The conventions are in `docs/AGENTS.md`.

## Current state

Last updated: 2026-10-27

| Milestone                       | State       | Note                                                  |
| ------------------------------- | ----------- | ----------------------------------------------------- |
| M0 -- Foundation                | done        | exit signed off 2026-08-25                            |
| M1 -- v2 query                  | done        | closed 2026-09-21                                     |
| M2 -- Tables, reader and ingest | done        | closed 2026-10-12                                     |
| M3 -- v2 auth                   | done        | closed 2026-10-14                                     |
| M4 -- Embedded DuckDB           | done        | closed 2026-10-16                                     |
| M5 -- Flight SQL                | done        | closed 2026-10-26                                     |
| M6 -- Hardening                 | not started | criteria below                                        |
| M7 -- Observability             | not started | criteria below                                        |
| M8 -- Exploration               | not started | criteria below                                        |
| M9 -- Drop-in compat            | not started | the v1 goldens are captured when it starts (ADR-0015) |
| M10 -- Release                  | not started |                                                       |

M6 criteria. Entry: M5 closed. Exit: the shutdown grace is a
configuration key with the current constant as its default, and the
drain of both planes runs under it; the server logs one warning at
startup when the HTTP listener is on and the cluster is secure, and the
round trip asserts the line in the secure server's log; readiness
answers `503` without a dial from the first moment of the drain
(ADR-0004), and the stress's drain step asserts it; the binary writes
`READY=1` to `$NOTIFY_SOCKET` once every listener is bound and
`STOPPING=1` when the drain starts, with the standard library and no
dependency, and does nothing when the variable is unset, and sends the
`WATCHDOG=1` keepalive at half of `WATCHDOG_USEC`, tied to the serving
loop and never to the cluster probe, from `READY=1` until the drain
starts; the stress's drain step (`docs/e2e.md`, Stress definition) is green by
hand on both clusters; the Handoff items M6 consumes are deleted.

M7 criteria. Entry: M6 closed. Exit: `GET /metrics` serves the
Prometheus exposition on the HTTP and HTTPS mux outside the bearer edge,
switched by `metrics.enabled` with true as the default (ADR-0004), with
request and query latency, rows and bytes streamed per format, time to
first byte, pool and breaker state, retry counts, qdb call durations, Go
runtime and build info, on both planes where the plane has the concept,
and a user label on the pool gauges only; every query on both planes
logs one line with the engine, the plane, the duration, the row count
and the outcome at `info`, and the statement text, truncated, at
`debug`; `net/http/pprof` serves under `/debug/pprof/` on the same mux
when `debug.pprof` is true and is absent when false, false the default;
the round trip scrapes `/metrics` after its requests and asserts that
every expected series exists; the stress's budget step is green by hand
on both clusters.

M8 criteria. Entry: M7 closed. Exit: the eight exploration routes of
the brief's sketch (`docs/brief.md`, /api/v2 endpoint sketch, M8 rows)
serve behind the bearer edge with ADR-0010's problems, over
`Cluster.Tables`, `Cluster.TableSchemas`, a find core and a statistics
core in `internal/qdb`; the round trip lists its tables under a prefix,
reads one schema with its tags, attaches a tag, lists the tag and its
entries, detaches it, and reads the cluster and one node, each compared
against what it created; the Handoff item naming the table cores is
deleted.

In flight:

- `sc-19567/rr-readiness-drain`: readiness answers 503 without a dial
  for the whole drain, built and reviewed, held before its merge
  (owner, 2026-10-27). It rebases onto the base once the DuckDB 2.0.0
  unit has merged and merges once a build of its head is green.

Next:

1. `/rr-start` M6: the systemd notify protocol, `READY=1` once every
   listener is bound and `STOPPING=1` when the drain starts, written to
   `$NOTIFY_SOCKET` with the standard library and silent when the
   variable is unset; the HTTP bind moves out of `ListenAndServe` so
   READY follows the bind.
2. `/rr-start` M6: the stress's drain step (`docs/e2e.md`, Stress
   definition) green by hand on both clusters, with its server run under
   a long grace through `listen.shutdown_grace`, and the close of M6, as
   one slice.
3. Investigate the intermittent Go runtime fault `found bad pointer in
Go heap`, raised inside arrow-go's C data import of a string column
   while the 2.0.0 binding converts a DuckDB chunk, under the flightsql
   statement test on the secure cluster (Linux haswell, qdb-api-rest
   build 188, passed on retry). It is the first such fault on the 2.0.0
   line, the Go side of the path is unchanged across the bump, and the
   repository's code on it consumes each batch synchronously, so the
   suspects are the binding's C layer and the library. First step: the
   test in a loop under the race detector and `GODEBUG=invalidptr=1`
   on Linux amd64.

Handoff to M6 and M7:

- The round trip drives both servers over plain HTTP, and no step reads
  the HTTPS listener. Steps over both HTTP and HTTPS may be added
  (owner, 2026-10-24, and left open on 2026-10-26); `docs/e2e.md`, The
  driver, says which planes the driver takes today.
- The stress has no Flight case (owner, 2026-10-13). The drain covers
  Flight streams (`cmd/qdb_rest/main.go`, `shutdown`).
- `Cluster.Tables` and `Cluster.TableSchemas` (`internal/AGENTS.md`,
  Code) are the cores `GET /api/v2/tables` and
  `GET /api/v2/tables/{name}` wrap. M8's.
- Query capture on both planes is M7's (owner, 2026-10-20, shaped
  2026-10-26 as the M7 criteria say). Nothing logs query text today;
  the HTTP and Flight access lines carry no query.
- Two flaky failures on the `#sql` path are observed in CI and not
  understood (owner, 2026-10-22). Both come from qdb-duck's own qdbd
  sessions, on a table created seconds earlier through this server's
  sessions, and a retry of the same commit passes: `Catalog Error:
Cannot get shards for table ...: The handle is invalid.` (the Go
  tests on macOS, builds 158 and 165) and `Catalog Error: Failed to
fetch data from Quasar: The timeseries does not contain this
column.` (the e2e round trip on Windows, builds 164 and 165). The
  REST pool discards a bad session and retries a read once; whether
  qdb-duck does either is unknown. The owner now owns qdb-duck and
  investigates there, and suspects qdb-duck's table cache TTL, which
  upstream takes as a parameter (`docs/brief.md`, Risks, 5), for the
  second failure (owner, 2026-10-26). No milestone claims the
  investigation yet.

Blocked on:

- The merge of `sc-19567/rr-readiness-drain` waits on its rebase and a
  green build of its head.

## Entries

## 2026-10-27 -- the watchdog keepalive is part of M6

- Owner decision; `docs/brief.md`, Milestones, M6. The keepalive proves
  the serving loop and never the cluster.

## 2026-10-27 -- readiness-drain-plan.md deleted

- Facts moved to `internal/httpapi/AGENTS.md` (Middleware),
  `cmd/qdb_rest/AGENTS.md` and the comments of `internal/httpapi` and
  `cmd/qdb_rest`.

## 2026-10-27 -- duckdb-2-plan.md deleted

- Facts moved to ADR-0017 (Decisions 2 and 3, Alternatives),
  `third_party/AGENTS.md`, `internal/encoding/AGENTS.md` and the
  comments of `internal/olap`, `internal/encoding` and `.envrc`.

## 2026-10-26 -- M6 gains the systemd notify protocol and readiness 503 during the drain, M7 gains pprof

- Owner decisions; `docs/brief.md`, Milestones (M6, M7) and
  Observability and logging, and ADR-0004, rule 2. The watchdog
  keepalive is open (M6 criteria).

## 2026-10-26 -- m6-grace-warning-plan.md deleted

- Facts moved to `docs/brief.md` (Observability and logging),
  `docs/e2e.md` (The v2 round trip, The driver), `tests/e2e/AGENTS.md`
  and the comments of `cmd/qdb_rest`, `internal/config` and `tests/e2e`.

## 2026-10-26 -- M8 exploration defined

- Owner decisions; `docs/brief.md`, /api/v2 endpoint sketch (M8 rows
  and the paragraph below them) and Milestones. The criteria are in
  Current state.

## 2026-10-26 -- resilience split into M6 hardening and M7 observability; no server-side deadlines; the stress and the v1 goldens run by hand

- Owner decisions; `docs/brief.md`, Milestones, Resilience and
  connection management, Testing doctrine, and Risks (5). ADR-0015
  edited in place for the numbering and the Buildkite rule. The
  criteria of both milestones are in Current state.

## 2026-10-26 -- the validate divergences filed with R&D

- sc-20056 tracks qdbd's validate disagreeing with its own execution
  (ADR-0018, Decision 9): a bare NULL literal and an empty non-select
  column type differently. sc-20057 asks for `qdb_query_validate_arrow`,
  the statement's Arrow schema from the function `qdb_query_arrow` uses
  for its own result, so the binding's tag-to-Arrow mapping can go.

## 2026-10-26 -- M5 closed; sql-last-result-plan.md deleted

- M5 exit criteria met by the tree and build 175 on the base head. Facts
  moved to ADR-0018 (Decision 12) and the `AGENTS.md` files of
  `internal/olap`, `internal/httpapi` and `internal/flightsql`.

## 2026-10-25 -- prepared-statements-plan.md deleted; the described schema, prepared statements and the JDBC smoke steps landed

- Facts moved to ADR-0018 (Decisions 9, 10, 12 and 14), the brief
  (Arrow Flight SQL), `docs/e2e.md`, the `AGENTS.md` files of
  `internal`, `internal/olap`, `internal/flightsql` and `tests/e2e`, and
  the comments of `internal/olap`, `internal/qdb`, `internal/query` and
  `internal/flightsql`.

## 2026-10-24 -- a plain-text warning for HTTP on a secure cluster, and HTTPS steps as an option

- Owner decision; Current state, Handoff. The warning is a requirement,
  the HTTPS steps of the round trip a possibility.

## 2026-10-24 -- flight-tests-plan.md deleted; the Flight client in the equivalence property and the ADBC smoke steps landed

- Facts moved to `docs/e2e.md`, ADR-0018 (Decision 14),
  `internal/AGENTS.md` (Building, Tests), `internal/flightsql/AGENTS.md`
  (Tests), `internal/httpapi/AGENTS.md` (Tests), `tests/e2e/AGENTS.md`
  and the comments of `internal/qdbtest/flight`,
  `internal/httpapi/roundtrip_test.go`, `tests/e2e/tools/e2etool/main.go`
  and `tests/e2e/round-trip.sh`.

## 2026-10-24 -- the JDBC smoke step waits on prepared statements

- Owner decision; ADR-0018 (Decision 14) and `docs/e2e.md`, The Flight
  SQL smoke steps. Arrow's JDBC driver sends every statement as
  `CreatePreparedStatement`.

## 2026-10-23 -- flight-tables-schema-plan.md deleted; GetTables with include_schema landed

- Owner decisions moved to ADR-0018 (Decision 11), `internal/AGENTS.md`
  (Code, Tests), `internal/flightsql/AGENTS.md` (Catalog, Tests) and
  the comments of `internal/qdb/table.go` and
  `internal/flightsql/catalog.go`.

## 2026-10-22 -- flight-catalog-plan.md deleted; GetSqlInfo and the catalog commands landed

- Owner decisions moved to `internal/flightsql/AGENTS.md` (Catalog),
  `internal/AGENTS.md` (the table list) and the comments of
  `internal/qdb/table.go` and `internal/flightsql`.

## 2026-10-22 -- flight-statement-plan.md deleted; the statement query on the Flight listener landed

- Owner decisions moved to `internal/flightsql/AGENTS.md`,
  `internal/AGENTS.md`, `internal/encoding/AGENTS.md`,
  `internal/model/AGENTS.md`, ADR-0018 (Decision 5) and the comments of
  `internal/flightsql`, `internal/qdb` and `internal/encoding`.

## 2026-10-20 -- flight-listener-plan.md deleted; the Flight listener with Handshake and bearer auth landed

- Owner decisions moved to `internal/flightsql/AGENTS.md`,
  `internal/AGENTS.md` (Code, Building), `cmd/qdb_rest/AGENTS.md` and
  the comments of `internal/flightsql` and `cmd/qdb_rest`. Query
  capture is M6's (Handoff). The `qdb-documentation` work is outside
  this project and follows the official release (owner, 2026-10-20),
  so the `tls_port` item left M5 here, in `docs/brief.md` and in
  ADR-0018.

## 2026-10-20 -- roundtrip-read-range-plan.md deleted; the round trip reads under its rows' span

- The bounded read and its reason went to `internal/httpapi/AGENTS.md`,
  Tests, and `internal/AGENTS.md`, Tests; the span itself is the
  fixture's `Range`. The owner keeps the pending reproduce for R&D out
  of the tree (owner, 2026-10-20).

## 2026-10-20 -- query-dispatch-plan.md deleted; one query route over both engines

- Owner decisions moved to ADR-0010, ADR-0017 and ADR-0018 (edited in
  place), `internal/AGENTS.md`, `internal/httpapi/AGENTS.md` and the
  comments of `internal/query` and `internal/qdb`.

## 2026-10-19 -- every qdbd test runs on both clusters; secure-data-path-plan.md deleted

- Owner decisions moved to `internal/AGENTS.md` (Code, Tests),
  `internal/httpapi/AGENTS.md`, ADR-0010 and the comments of
  `internal/qdb`, `internal/qdbtest` and `internal/httpapi`.

## 2026-10-19 -- M5 gains smoke tests through Arrow's Flight SQL ADBC and JDBC drivers

- Owner decision; ADR-0018, Decision 14, edited in place, and
  `docs/e2e.md`, The Flight SQL smoke tests.

## 2026-10-17 -- the text encoders write every DuckDB scalar; duckdb-scalars-plan.md deleted

- Owner decisions moved to `internal/encoding/AGENTS.md`,
  `internal/olap/AGENTS.md`, `internal/httpapi/AGENTS.md` and the
  comments of `internal/encoding`. The qdb-duck pin is gone.

## 2026-10-16 -- M4 closed: sc-19567/rr-m4-sql merged

- The owner's review accepted the code's four departures from the plan,
  which the comments of `Cluster.SQL`, `Cluster.credentialsOf`,
  `Instance.Query` and `checkSQLFormats` record.

## 2026-10-15 -- M4 built: POST /api/v2/sql over one DuckDB instance per user; m4-sql-plan.md deleted

- Owner decisions in ADR-0017 (Decision 11 edited in place) and the
  plan's Rationale, moved to `internal/olap/AGENTS.md`,
  `internal/AGENTS.md`, `internal/httpapi/AGENTS.md`, `docs/e2e.md` and
  the comments of `internal/config`, `internal/olap` and `internal/qdb`.

## 2026-10-14 -- M4 started: DuckDB linked on every platform, nothing routed

- Owner decisions: the binding's prebuilt libraries are replaced by
  stubs (`third_party/AGENTS.md`), the link tag follows the library per
  OS (ADR-0017, Decision 2, edited in place), `--version` prints the
  DuckDB release (`cmd/qdb_rest/AGENTS.md`), and ADRs are edited in
  place when a detail proves wrong (`docs/AGENTS.md`, ADRs).

## 2026-10-14 -- M3 closed: the token grant, refresh, logout and session landed; v2-auth-plan.md deleted

- Owner decisions in ADR-0016 and the plan's Rationale, moved to
  `internal/httpapi/AGENTS.md`, Handlers, `internal/build/build.go` and
  the comments of `internal/auth/token.go`.

## 2026-10-13 -- M5 designed: ADR-0018 accepted, one Flight listener, the `#sql` prefix on both planes, stateless tickets

- Owner decisions; ADR-0018, and `docs/brief.md`, Arrow Flight SQL,
  carries the result. The Flight e2e question of `docs/e2e.md` is
  closed with no e2e step.

## 2026-10-13 -- M4 designed: ADR-0017 accepted, one DuckDB instance per user

- Owner decisions; ADR-0017, and `docs/brief.md`, Embedded DuckDB,
  carries the result. Open question 2 is closed.

## 2026-10-13 -- M3 started: ADR-0016 accepted, token lifetimes and the refresh, logout and session shapes

- Owner decisions; ADR-0016, and `docs/brief.md`, Authentication,
  carries the result. Open question 1 is closed.

## 2026-10-12 -- early-preview scope: milestones reordered, bench, dataset and v1 goldens removed

- Owner decision; ADR-0015. The brief's Milestones and Testing doctrine
  and `docs/e2e.md` carry the result, and ADR-0013 is superseded.

## 2026-10-12 -- M2 closed: the round trip runs in every build step; three e2e plans deleted

- `scripts/cicd/40.test-e2e.sh` runs `make test-round-trip` after the Go
  tests on every platform. Owner decisions: the tool builds into `bin/`,
  the harness Makefile's trap is the one stop of the REST servers, and M3
  starts with the next unit. `ci-e2e-plan.md`, `e2e-round-trip-plan.md`
  and `e2e-v2-round-trip-plan.md` deleted; facts moved to `docs/e2e.md`,
  decision logs of 2026-10-11 and 2026-10-12.

## 2026-10-12 -- the v2 e2e is the round trip, and its surface is defined

- Owner decisions: the name is "round trip", the sequence of
  `TestRoundtrip` over HTTP; the round trip reads and queries
  (`SELECT *`) four tables in every format under identity and gzip,
  and the empty read, zstd, request codings and push modes stay Go
  tests (`docs/e2e.md`, "The v2 round trip").

## 2026-10-12 -- the basic v2 e2e flow landed; e2e-basic-flow-plan.md deleted

- `make -C tests/e2e test-flow` runs on both clusters. Facts moved to
  `tests/e2e/AGENTS.md` and ADR-0014's Consequences; the rest is in the
  tool's and the driver's comments.

## 2026-10-12 -- three qdbd defects filed with R&D

- sc-19942, sc-19944 and sc-19939 are R&D's to fix, and the test
  failures they cause are in `internal/AGENTS.md`, Tests.

## 2026-10-11 -- the columnar JSON decoder landed; json-ingest-plan.md deleted

- Facts moved to `internal/encoding/AGENTS.md`, The seam, and the
  comments of `internal/encoding/json.go`.

## 2026-10-11 -- the ingest takes columnar JSON; the e2e flow starts with one basic test

- Owner decisions: `POST /api/v2/rows` accepts the columnar JSON the
  read answers (`docs/brief.md`, endpoint sketch and M2), the next unit
  implements it, the first e2e unit is one basic test and the surface
  is defined with the owner afterwards, the tool stays Go
  (`docs/e2e-v2-flow-plan.md`, "Owner decisions (2026-10-11)").

## 2026-10-09 -- the NDJSON and Arrow IPC decoders and the request codings landed; ingest-decoders-plan.md deleted

- `POST /api/v2/rows` reads CSV, NDJSON and Arrow IPC bodies under
  `Content-Encoding: gzip|zstd`. Owner decisions: the ingest cap bounds
  the bytes on the wire, a sparse NDJSON object reads absent as null, a
  body's Arrow types must equal the reader's, the text wires share one
  cell parser and the standard library parses NDJSON, and the decoders'
  faults are one property. The rules went to `internal/encoding/AGENTS.md`
  (The seam, Tests) and `internal/httpapi/AGENTS.md` (Handlers,
  Middleware, Tests).

## 2026-10-08 -- the prose audit is complete

- Every comment and document outside `tests/e2e/bench/bench.py` reads as
  plain sentences. Owner decisions: the remainder landed as one unit
  without a plan document, and `internal/model/AGENTS.md`,
  `tests/e2e/tools/package-dataset.sh` and the ADR template joined it.
  No fact moved.

## 2026-10-08 -- the internal/httpapi sources and AGENTS.md read as plain sentences; prose-audit-httpapi-plan.md deleted

- No fact moved, because the plan carried none. Owner decision: the
  audit lands in slices of one review each, and Current state, Next,
  item 1 names what remains.

## 2026-10-08 -- internal/qdb reads as plain sentences; prose-audit-qdb-plan.md deleted

- No fact moved, because the plan carried none. Owner decision: the
  rest of the audit is one unit, listed under Current state, Next,
  item 1.

## 2026-10-07 -- the files the encode-test unit touched read as plain sentences; prose-audit-touched-plan.md deleted

- Owner decisions: accepted ADRs and dated entries are reworded with
  their decisions unchanged, and the bench is outside the audit. No fact
  moved, because the plan carried none.

## 2026-10-06 -- the encoders' tests are encode_test.go; encode-test-plan.md deleted

- Owner decisions: the project says "encode", never "render", for the
  encoding concept, and JSON, NDJSON and CSV are the text formats
  (`internal/encoding/AGENTS.md`), and comments and documents read as
  plain sentences (root `AGENTS.md`, Prose).

## 2026-10-06 -- the table fixture holds one record batch; fixture-arrow-plan.md deleted

- The fixture pushes through the Arrow writer and is checked by array
  equality. The column-type map is the binding's
  (`TsColumnType.ArrowType`, qdb-api-go PR 126). Owner decisions:
  QuasarDB's column types stay the fixture's vocabulary, and
  `internal/model` imports no binding. The rules went to
  `internal/AGENTS.md` (Tests, the read) and `internal/encoding/AGENTS.md`
  (Tests).

## 2026-10-05 -- the ingest decodes to Arrow; arrow-ingest-plan.md deleted

- `POST /api/v2/rows` decodes its body in `internal/encoding` into one
  record batch per table and pushes through the Go API's Arrow writer.
  The neutral types live in the new `internal/model`. Owner decisions:
  Arrow is the one representation between the layers, and one body has
  one column list, types included, which the decoder checks. The rules
  went to `internal/AGENTS.md` (the ingest), `internal/encoding/AGENTS.md`
  (the decoder) and `internal/model/AGENTS.md`.

## 2026-09-23 -- the CSV ingest landed; ingest-csv-plan.md deleted

- `POST /api/v2/rows` landed with a CSV body through the writer, one
  push per request. Owner decisions: the tables of one body share one
  column list, and the httpapi tests are one round trip and one error
  table. The rules went to `internal/AGENTS.md` (the ingest) and
  `internal/httpapi/AGENTS.md` (the route, the round trip).

## 2026-09-23 -- the table reader landed; table-reader-plan.md deleted

- `GET /api/v2/tables/{name}/rows` landed through the bulk reader, in
  every format. The rules went to `internal/AGENTS.md` (the read, the
  lent batches, the two C API defects), `internal/encoding/AGENTS.md`
  (`EncodeStream`, the JSON array) and `internal/httpapi/AGENTS.md`
  (the route's contract).

## 2026-09-22 -- M2 gains the table reader; the ingest is multi-table

- Owner decisions: whole tables are read through the bulk reader
  (`GET /api/v2/tables/{name}/rows`), never materialized by a query.
  There is one ingest endpoint, `POST /api/v2/rows`, routed by
  `$table`, so the ingestion milestone folds into M2 and the later
  milestones renumber (`docs/brief.md`, Milestones). The contracts are
  the handlers'.

## 2026-09-21 -- tables-plan.md deleted with table create and delete landed

- The wire contract went to `docs/brief.md`, "Tables: create and
  delete", the handler rules to `internal/httpapi/AGENTS.md`, and the
  owner's ingest parameters to Current state, Next.

## 2026-09-21 -- M1 closed; M2 started with table create and delete

- Owner decisions: table creation and ingest come before any CI run of
  the e2e, and the table endpoints need no ADR, because their contract
  is the brief's (`docs/brief.md`, "Tables: create and delete").

## 2026-09-18 -- ADR-0014 accepted: the v2 e2e is a generated flow; M2 -- tables and ingest inserted

- Owner decisions: the v2 e2e proves login, create, query, ingest and
  query back with generated rows, with no goldens and with error rows
  as Go tests. The two endpoints move into a new M2 and the later
  milestones renumber (`docs/brief.md`, Milestones). `docs/e2e.md` and
  `docs/bench.md` are permanent specifications (`docs/AGENTS.md`).

## 2026-09-17 -- the status probes are outside the compatibility contract

- Owner decision: v1 is the login and the query. The probes keep both
  paths with no old-server promise and no golden (`docs/brief.md`,
  Compatibility contract and Observability and logging).

## 2026-09-17 -- the suites are `v1` and `v2`; no v1 golden selects a count

- Owner decisions: `legacy` leaves every suite, target, driver, fixture,
  package and bench-run name. No v1 golden exercises a deliberate
  deviation (ADR-0013). A v2 case is one request with its formats
  inside (`docs/e2e.md`, "The v2 suite").

## 2026-09-17 -- test-strategy-plan.md deleted with the documentation re-cut landed

- The decisions went to ADR-0013, the layers, the deviations and the
  milestones to `docs/brief.md`, and the mechanics to `docs/e2e.md`
  and `docs/bench.md`.

## 2026-09-17 -- ADR-0013 accepted: e2e goldens run in Buildkite

- Owner decisions: e2e returns to CI from M1. A golden is an audited
  response. Budgets leave the CI gates, and every number is the
  bench's (`docs/brief.md`, Testing doctrine).

## 2026-09-16 -- compression-plan.md deleted with response compression landed

- The wire rule went to ADR-0012, and the middleware rules to
  `internal/httpapi/AGENTS.md`.

## 2026-09-16 -- ADR-0012 accepted: v2 response compression

- Owner decisions: client order decides and there is no `q`. The level
  is the fastest, with one compressor per response. zstd lands with
  gzip, since `arrow-go` already vendors and links it (in the brief,
  zstd leaves M4 and open question 3 is closed).

## 2026-09-15 -- login-plan.md deleted with the login endpoint landed

- The wire contract and the credential check went to ADR-0011, and the
  handler rules to `internal/httpapi/AGENTS.md`.

## 2026-09-15 -- ADR-0011 accepted: v2 login

- Owner decisions: credentials are proven by one direct dial outside
  the pools, breaker-gated and unbudgeted. The token response is RFC
  6749's. `auth.access_ttl` lands now, with a default of 15m.

## 2026-09-15 -- query-plan.md deleted with the query endpoint landed

- The wire contract went to ADR-0010, and the handler rules and the
  error mapping to `internal/httpapi/AGENTS.md`.

## 2026-09-15 -- ADR-0010 accepted: v2 query request, negotiation and errors

- The query is the body, `Accept` picks the encoder, errors are RFC 9457
  problems with the status saying who failed, and only bearer access
  tokens are accepted.

## 2026-09-12 -- the full-table text/csv equivalence leaves the harness

- Owner decision: it is not a target, and cross-format correctness is
  the Go property test's (`docs/brief.md`, Testing doctrine). The awk
  comparator and the section that specified it leave `docs/e2e.md`.

## 2026-09-11 -- encoders-plan.md deleted with the text encoders landed

- The text-format rules went to `internal/encoding/AGENTS.md`, which now
  holds every encoder rule, and the columns-only JSON shape and the
  `jsontext` appenders to `docs/brief.md`.

## 2026-09-11 -- arrow-query-plan.md deleted; ADR-0009 leaves the tree

- The query core hands out the batch `qdb_query_arrow` builds, so the
  wire schema is the binding's, not a REST decision. The ownership rule
  went to `internal/AGENTS.md`, Code, and the encoder rules to
  `internal/encoding/AGENTS.md`.

## 2026-09-10 -- table-fixture-plan.md deleted with the fixture landed

- The fixture rules and the writer's null contract went to
  `internal/AGENTS.md`, Tests, and the upstream request to Current
  state, Next.

## 2026-09-08 -- m1-plan.md deleted with the Arrow unit landed

- The wire types went to ADR-0009, the `count` deviation to
  `docs/brief.md`, Compatibility contract, and the result-set and
  vendoring rules to `internal/AGENTS.md`. Later M1 units bring their
  own plan.

## 2026-09-08 -- ADR-0009 accepted: Arrow wire types for query results

- Timestamp(ns, UTC), Utf8 and Binary, every field nullable, zero-copy
  from the binding's result set.

## 2026-09-08 -- count answers as int64 in v1

- Owner decision: a `count` column is an int64 and clients never told
  them apart, so v1 answers `"type":"int64"` where the old server said
  `"count"`. `docs/brief.md`, Compatibility contract.

## 2026-09-04 -- Session fate is the binding's; ADR-0003 removed

- Owner decision: `qdb-api-go` decides a session's fate (`IsBadSession`,
  through `Lease.Done`). This layer keeps the breaker (fed by
  `IsClusterUnavailable`), the budget, the per-user map and the opt-in
  read retry. Session health leaves `docs/brief.md`, Resilience.

## 2026-09-02 -- v2 auth precedes the drop-in; the find core lands with the wrappers

- Owner decision: M2 is v2 auth, M3 the drop-in, so the first shippable
  binary exposes no half-built v2 endpoint. The `find` wart's v2 core
  is written in M3. `docs/brief.md`, Milestones.

## 2026-09-02 -- admission control and the sentinels leave the scope

- Owner decision: the session budget is the whole overload mechanism.
  There is no admission layer and no 429. The unreachable `"(void)"` /
  `"(undefined)"` sentinels are not reproduced. `docs/brief.md`,
  Resilience and Compatibility contract.

## 2026-09-02 -- milestones re-cut into nine smaller ones

- Owner decision: M1 narrows to the v2 query path plus a minimal login.
  The remaining v2 surface, resilience, Flight SQL, ingestion and DuckDB
  become M3..M8 with one green bar each. `docs/brief.md`, Milestones.

## 2026-09-02 -- ADR-0007 accepted: v1 compatibility layer

- v2 comes first, every v1 route wraps its v2 counterpart, and v1 code
  lives in `internal/httpapi/v1` only. The direct v1 login leaves the
  tree and returns as a wrapper in M2.

## 2026-09-02 -- ADR-0008 records the /api/v1 spelling decision

- The 2026-09-01 owner decision (canonical `/api/v1/<path>`, unversioned
  aliases, never a redirect) moves from the brief into ADR-0008.

## 2026-09-02 -- milestones reordered: v2 data plane before drop-in compat

- Owner decision: v1 routes wrap v2, so v2 is built first and the
  v1 endpoints follow as thin wrappers in their own package
  (`docs/brief.md`, Milestones, and ADR-0007). The direct v1-query
  implementation and its plan were discarded. The verified wire facts
  moved to `docs/e2e.md`.

## 2026-09-02 -- /api/v1/tags dropped from scope

- Owner decision: it is unused, so it is removed from the compat
  surface, the goldens and the code. `docs/brief.md`, Compatibility
  contract.

## 2026-09-01 -- canonical spelling is /api/v1

- Owner decision: internal references always spell v1 endpoints
  `/api/v1/<path>`. The unversioned aliases are compatibility-only.
  ADR-0008.

## 2026-09-01 -- ADR-0005 accepted: token cryptography

- Hand-rolled dir+A256GCM compact JWE, argon2id + HKDF derivation,
  go-jose as the test-side cross-check. `internal/auth` and v1
  `/api/v1/login` land with it.

## 2026-09-01 -- ADR-0006 accepted: clock injection

- The clock is a `now func() time.Time` constructor argument, never
  context state. synctest is deferred to future pure-Go timer units.

## 2026-08-31 -- v1 aliases minted; v1 routes wrap v2

- Owner decision: every v1 endpoint is also served at `/api/v1/<path>`,
  unversioned paths assume v1, and new endpoints go under `/api/v2/*`
  only. v1 routes wrap v2 implementations, with no parallel code. `docs/brief.md`,
  Compatibility contract.

## 2026-08-31 -- Per-call deadlines are a binding concern, not the gateway's

- Owner decision: the abandon-on-deadline failsafe, session poisoning,
  the wedged counter and `pool.call_timeout` leave `internal/qdb`. Calls
  are bounded by the C socket timeout. If wanted, the capability belongs
  upstream (`qdb-api-go` or the C API). ADR-0003.

## 2026-08-31 -- Pool unit landed; ADR-0003 and ADR-0004 accepted

- `pool-plan.md` was deleted. Its facts moved to ADR-0003 (C API and qdbd
  constraints), `internal/AGENTS.md` (session-exhaustion test rule) and
  the Handoff block (bench buffer size, login handoff).

## 2026-08-30 -- Pool core taken from qdb-api-go

- Owner decision: the REST API integrates the binding's `SessionPool`
  and `SessionFactory`. The local core is dropped and eager option
  validation is out. ADR-0003.

## 2026-08-28 -- Pool review decisions

- Owner decisions: QuasarDB's vocabulary (user, session), every key in
  every layer, option checks left to the binding, the failsafe untested
  against the C API, and a session factory upstream. Recorded in
  `docs/brief.md`, Vocabulary, `cmd/qdb_rest/AGENTS.md` and ADR-0003.

## 2026-08-27 -- Pool plan review decisions

- Owner decisions: pools are keyed per user, not per session (brief
  amended, "Resilience and connection management"), C calls reach code
  only through a narrow `Handle` wrapper, and probes get their own
  ADR-0004 (accepted 2026-08-31). ADR-0003.

## 2026-08-26 -- Pool plan decisions

- Owner decisions: readiness dials its own handle and fails with `503`
  (brief contract amended), `IsRetryable` classifies errors, and nothing
  dials at startup. ADR-0003 and ADR-0004.

## 2026-08-25 -- M0 complete, M1 started

- Owner exit sign-off for M0. The M1 criteria are in Current state, and
  the scope is in `docs/brief.md`, Milestones.

## 2026-08-25 -- M0 scope complete

- Every M0 deliverable is landed and green on Buildkite for all eight
  platforms. The owner's exit sign-off is outstanding (Current state).

## 2026-08-24 -- e2e harness excluded from CI

- Owner decision, until further notice. The record and the re-adding
  recipe are in `.buildkite/AGENTS.md`.

## 2026-08-24 -- Packaging and Docker removed from the plan

- Owner decision: deb/rpm packaging and the Docker image leave the
  milestones entirely. `docs/brief.md`, Non-goals.

## 2026-08-24 -- Bench methodology decisions

- Owner decisions: `native@qdbd` measures `stream_query()` only, C API
  compression is pinned per run with the default `none`, and a run is 3
  warmups plus 5 measured reps with the median reported.
  `docs/bench.md`, decision log 2026-08-24.

## 2026-08-23 -- ADR-0002 accepted: context-carried logging

- The logger travels in `context.Context`. HTTP middleware tags request
  ids and writes the access line. The house rules are in
  `internal/AGENTS.md`.

## 2026-08-21 -- De-risk spikes removed from the roadmap

- Owner decision: the Flight SQL driver-compatibility and go-duckdb
  embedding spikes are dropped and neither topic is a risk. M3 and M4
  start on dependency order alone. `docs/brief.md`, Milestones and Risks.

## 2026-08-21 -- Go 1.27 adopted

- The toolchain is bumped. The 1.27 features blessed for this project are in
  `docs/brief.md`, Development standards.

## 2026-08-21 -- ADR-0001 accepted: TLS certificates

- A PEM pair from config, with an ephemeral self-signed default. ACME,
  ACM and hot reload are deferred.

## 2026-08-21 -- Dataset archive published on S3

- `make -C tests/e2e load` fetches `reproduce-2026-08-19-5613032.tar.gz`
  through `datasets.json`. No local copy is needed anywhere.

## 2026-08-20 -- M0 started

- The entry criteria are met. The scope is in `docs/brief.md`,
  Milestones, and the criteria are in Current state.

## 2026-08-20 -- Bench Phase 1 accepted

- `native@qdbd` and `v1@old-rest` fingerprints agree on every query.
  The tool is ready for `v1@new-rest`. The contract decisions are in
  `docs/bench.md`, decision log 2026-08-20.

## 2026-08-19 -- Bench measures two data volumes

- qdbd -> reducer and reducer -> client, with a reduce-shape query family.
  `docs/bench.md`, "Two volumes" and decision log 2026-08-19.

## 2026-08-19 -- e2e harness in place; M1 red bar exists

- The dataset is loaded and round-trip verified, the v1 goldens are
  captured from `master`, and `make test-v1` fails fast without a
  server under test.
  `docs/e2e.md`, decision log 2026-08-19.

## 2026-08-16 -- Planning frozen

- The e2e harness and bench plans are approved. The decision logs of
  2026-08-16 are in `docs/e2e.md` and `docs/bench.md`.

## 2026-08-14 -- Old-server baseline measured

- A pre-harness spike. The bench's `v1@old-rest` result files supersede

  its numbers (`docs/bench.md`).
