# DuckDB queries count against the session budget

Thread: rr: DuckDB queries count against the session budget
Status: draft

## Outcome

When the unit lands, every DuckDB call holds one unit of the
process-wide session budget (`pool.max_sessions`) for as long as it
runs. A DuckDB call is a `#sql` query (`Cluster.Duck`) or a DuckDB
describe (`Cluster.DuckDescribe`). The unit is taken before the user's
instance opens, so the first call's attach is counted too. A call that
finds the budget full waits in the same queue as a native dial, until a
unit frees or its caller leaves. `qdb_rest_budget_sessions` gains a
`state="duckdb"` series, the part of `in_use` that DuckDB calls hold.
The stress's budget step reads through `#sql` next to the native
clients, under a budget of three, and fails if `in_use` exceeds `max` or
no poll sees a DuckDB unit. ADR-0017 Decision 7, the brief, two
`AGENTS.md` files, the reference config and `docs/e2e.md` describe the
new behaviour. The log's two stale open items are deleted: the Flight
stress case and the DuckDB budget.

The unit leaves these for later:

- qdb-duck's idle handles stay open until the instance closes, and
  nothing counts them.
- A query that reads several QuasarDB tables leases one handle per table
  reference and still holds one unit.
- The `duckdb-go-bindings` bump (story 20145) waits for qdb-duck's
  upgrade to DuckDB 2.0.1, which runs in qdb-duck, and stays out of this
  unit. Story 20145 stays open. The log's Blocked on entry is left as
  it is.

## Verified facts

1. The budget is a buffered channel. `acquire` blocks until a slot
   frees or `ctx` ends and then returns `ctx.Err()`
   [`internal/qdb/budget.go:17-24`]. `inUse` is `len` and `max` is
   `cap` [`internal/qdb/budget.go:27-28`].
2. A native dial takes its unit in `connect`, before the dial, and the
   pool's closer gives it back after `qdb_close` returns
   [`internal/qdb/cluster.go:196-243`].
3. Every DuckDB call goes through `withInstance`, which counts the call
   in `up.duckIn` under `c.mu`, opens the instance through `instanceOf`,
   runs `f`, and uncounts the call in a defer
   [`internal/qdb/cluster.go:743-772`]. `Duck` and `DuckDescribe` are its
   only callers [`internal/qdb/cluster.go:784,799`]. `query.Run` and
   `query.Describe` are their only callers
   [`internal/query/query.go:150,168`].
4. `instanceOf` takes `up.duckMu` before `c.mu`
   [`internal/qdb/cluster.go:673-684`]. The collector takes `c.mu` and
   never `duckMu` [`internal/qdb/collector.go:50-52`]. The instance
   open's attach dials the cluster [`internal/qdb/cluster.go:686-691`].
   A failed attach on a secure cluster makes one unbudgeted classifying
   dial [`internal/qdb/cluster.go:703-710`].
5. qdb-duck's table scan leases one handle at bind and reads every
   shard through it [`qdb-duck/src/extension/quasar_reader.cpp:22-29,257`,
   read 2026-11-17 and again 2026-11-26]. The brief says that "a scan
   fans out to one DuckDB worker per four shards" [`docs/brief.md:575`].
   The tree contradicts it, and the brief is corrected.
6. A DuckDB describe binds the query, so a QuasarDB table in it leases
   a handle [fact 5]. A native describe holds a budgeted session through
   `ObservedCall` [`internal/qdb/cluster.go:556-560`].
7. `budget_sessions` has the states `in_use` and `max`
   [`internal/qdb/collector.go:33,86-87`].
8. The stress runs with `--pool-max-sessions 2 --pool-per-user-max 2`
   [`tests/e2e/Makefile:183`]. One user's idle native sessions keep
   their units until `pool.idle_timeout`, 5 minutes by default
   [`internal/config/config.go:108,235`, `internal/qdb/cluster.go:247-252`].
9. The round trip reads a table through DuckDB with
   `#sql SELECT '<table>' AS "$table", * FROM qdb.<table> ORDER BY "$timestamp"`
   and compares the decoded CSV, because qdb-duck's scan answers no
   `$table` column [`tests/e2e/round-trip.sh:151-169`].
   `compare_csv ... tool` decodes the body before it compares
   [`tests/e2e/common.sh:318-331`].
10. Story 20276 carries this unit. Its branch is
    `sc-20276/rest-duckdb-queries-count-against-the-session-budget`.

## Design

### `internal/qdb/budget.go`

```go
// budget is the process-wide ceiling on live sessions. [budget.go:5-7]
// A native session holds a unit from before its dial until its close returns. [budget.go:5-7]
// A DuckDB call holds a unit for as long as it runs, and duck counts those units. [Rationale 1]
type budget struct {
	// style: doc comment + inline notes
	// because: the new field needs one line on what it counts
	//
	// at duck: the DuckDB part of the units in use, read by the collector without a lock [collector.go:50-52]
	tokens chan struct{}
	duck   atomic.Int64
}
```

```go
// acquireDuck takes a unit for a DuckDB call, waiting as acquire does. [budget.go:17-24]
func (b *budget) acquireDuck(ctx context.Context) error {
	// style: doc comment + inline notes
	// because: one ordering constraint
	//
	// at duck.Add(1): counted after the take, so duck never exceeds inUse [Rationale 5]
}
```

```go
// releaseDuck gives a DuckDB call's unit back.
func (b *budget) releaseDuck() {
	// style: doc comment + inline notes
	// because: one ordering constraint, the mirror of acquireDuck
	//
	// at duck.Add(-1): uncounted before the release, so duck never exceeds inUse [Rationale 5]
}
```

```go
func (b *budget) duckInUse() int {
	// style: doc comment alone
	// because: an accessor like inUse and max
}
```

### `internal/qdb/cluster.go`

The `budget` field comment on `Cluster` (`:54-56`) is rewritten whole:

```go
// budget caps the sessions of the whole process at max_sessions. [config.go:106]
// A native session holds a unit from before its dial until its close returns, [cluster.go:196-243]
// and a DuckDB call holds one for as long as it runs. [Rationale 1]
```

```go
// withInstance runs f on the user's DuckDB instance, opening it on the user's first call. [cluster.go:739-742]
// The call holds one unit of the budget and counts as use of the user's pool until f returns. [Rationale 1]
// An error before f runs comes from the budget wait or instanceOf, and f's error is returned unchanged. [budget.go:17-24]
func (c *Cluster) withInstance(ctx context.Context, u User, f func(*olap.Instance) error) error {
	// style: doc comment + overview + inline notes
	// because: the unit, the pool count and the open must be taken and given back in one order on every path
	//
	// 1. find or create the user's pool and count the call, under one hold of mu [cluster.go:748-758]
	// 2. take a budget unit, waiting for one as a native dial does [Rationale 2]
	// 3. get the user's instance, opening it on the first call [cluster.go:765-768]
	// 4. run f on the instance [cluster.go:770]
	// 5. give the unit back and uncount the call, whatever happened [cluster.go:760-763]
	//
	// 1. no eviction pass sees the pool between its creation and the count [cluster.go:748-749]
	// 2. the unit is taken before the open, because the attach dials qdbd [Rationale 3]
	// 2. the call counts as use of the pool while it waits, so its pool is not evicted under it [cluster.go:838]
	// 5. the unit goes back before the call is uncounted, so an eviction pass never finds a pool whose call still holds a unit [proposal]
}
```

The `Duck` doc comment (`:774-782`) is rewritten whole. Its last
sentence, "The budget does not count it, because qdb-duck pools its own
qdbd sessions", is dropped:

```go
// Duck runs a DuckDB query on the user's own DuckDB instance and hands its batches to the sink. [cluster.go:774-776]
// The call holds one budget unit while it runs, and it is never retried and does not feed the breaker. [Rationale 1, ADR-0017 Decision 9]
// An error before the sink runs is ctx's while the call waits for a unit, then one of the errors it lists today; the sink's error is returned unchanged. [cluster.go:776-781]
```

The `DuckDescribe` body comment (`:796-797`) is rewritten whole:

```go
// The describe holds a budget unit and counts as use of the pool, as Duck does, because DuckDB binds the query and binding a table leases a qdbd handle. [Verified 6, Rationale 4]
```

### `internal/qdb/collector.go`

```go
// at the budget Desc's help: "Sessions of the process-wide budget: in use, of those held by DuckDB calls, and the maximum." [Rationale 5]
```

In `Collect`, step 1 gains a series and its comment is rewritten whole:

```go
	// 1. In use counts a session until its close returns, so it can exceed what the pools report while an evicted pool closes. [collector.go:84-85]
	// 1. duckdb is the part of in_use that DuckDB calls hold. [Rationale 5]
	gauge(k.budget, k.c.budget.duckInUse(), "duckdb")
```

The overview line 1 of `Collect` becomes "the budget: in use, DuckDB's
part of it, and max".

### `examples/qdb_rest.yaml`

```yaml
#pool:
# - Cluster sessions the server may hold across all users. A sql call holds one while it runs. [Rationale 1]
#max_sessions: 64
```

### `tests/e2e/Makefile`

`STRESS_FLAGS` becomes `--pool-max-sessions 3 --pool-per-user-max 2`.
The parameter comment (`:173-176`) is rewritten whole for the budget:

```make
# - The budget is three sessions and the per-user cap two, so one unit stays free for the budget step's sql clients while idle native sessions keep theirs. [Verified 8, Rationale 6]
# - The two native sessions are the smallest that leave the ingest and the read one each. [Makefile:173-174]
# - STRESS_CLIENTS is three times the per-user cap, not measured, so the clients queue for sessions. [Makefile:175-176]
```

### `tests/e2e/stress.sh`

```bash
# sql_reads <out> <url> <token> <table> <clients>: POST the round trip's
# DuckDB select of the table as CSV from <clients> curls at once through
# xargs -P, each body and its headers to <out>/sql-<i>.*, in the
# background with xargs's pid in SQL_PID.
sql_reads() {
    # style: doc comment + inline notes
    # because: one line a reader would ask about
    #
    # at the select: names the table as a constant, as the round trip's duckdb_back does, because qdb-duck's scan answers no $table column [round-trip.sh:151-157]
}
```

```bash
# poll_budget <out> <url> <pids...>: scrape /metrics until every reader ends, fail on the first scrape whose in_use exceeds max, and fail when no scrape saw a DuckDB unit. [Rationale 7]
poll_budget() {
    # style: doc comment + inline notes
    # because: the new assertion needs its reason
    #
    # at the duckdb check after the loop: a step that never saw a DuckDB unit would pass without showing that DuckDB calls are counted [Rationale 7]
}
```

```bash
# budget_step <out> <url> <token> <table> <csv> <clients>: the budget step of docs/e2e.md. [stress.sh:220-224]
# <clients> native clients and <clients> sql clients read one table at once while the gauge is polled. [Rationale 7]
# The gauge never exceeds the budget, a DuckDB unit is seen, and every body compares equal to the rows. [Rationale 7]
budget_step() {
    # style: doc comment + inline notes
    # because: two compare modes
    #
    # at the native compare: raw, because the reader's CSV is the generated bytes as they are [stress.sh:235-236]
    # at the sql compare: decoded through the tool, because DuckDB's CSV is not the generated bytes [common.sh:318-331, round-trip.sh:151-169]
}
```

`budget_gauge` reads the new state unchanged, because it takes the state
as an argument [`tests/e2e/stress.sh:183-186`].

### `docs/adr/0017-embedded-duckdb.md`, Decision 7

Decision 7 is rewritten in place, as if it had always read this way
(`docs/AGENTS.md`, ADRs):

```markdown
7. - qdb-duck's handle pool is sized and tuned by the `duckdb.quasar` block and the cluster block's network knobs, set as qdb-duck's globals on the attach connection before the ATTACH (owner, 2026-10-28).
   - A DuckDB call, a query or a describe, holds one unit of `pool.max_sessions` from before the instance opens until the call ends, and waits for it as a native dial does (owner, 2026-11-17, 2026-11-25).
   - The unit does not count against `pool.per_user_max` (owner, 2026-11-17).
   - Idle handles live until detach and are not counted, and a query over several tables holds one unit for several handles (owner, 2026-11-17).
```

### `docs/brief.md`

Embedded DuckDB, the "qdb-duck pools its own qdbd sessions" paragraph
(`:572-579`), is rewritten whole:

```markdown
- Each attached catalog holds up to `duckdb.quasar.max_pool_size` lazily opened `qdb_handle_t`, one hundred by default, one per operation in flight. [olap/AGENTS.md:18-19]
- A table scan leases one handle when it binds and reads every shard through it. [Verified 5]
- A DuckDB call holds one unit of `pool.max_sessions` while it runs (owner, 2026-11-17). [ADR-0017 Decision 7]
- Idle handles stay open until detach and are not counted. [ADR-0017 Decision 7]
```

Risks 5 (`:1153-1158`) is rewritten whole:

```markdown
5. - qdb-duck's handle pool never closes an idle handle before detach, and those handles are outside the budget. [ADR-0017 Decision 7]
   - A DuckDB call holds one budget unit however many handles it leases. [ADR-0017 Decision 7]
```

### `internal/AGENTS.md`

In the DuckDB call bullet (`:101-115`), the sentence "A DuckDB call is
never retried, does not feed the breaker, and takes no unit of the
budget, because qdb-duck pools its own qdbd sessions." becomes:

```markdown
- A DuckDB call is never retried and does not feed the breaker. [cluster.go:779-781]
- It holds one unit of the budget from before the instance opens until it ends, and waits for one as a native dial does (ADR-0017, Decision 7). [Rationale 1-3]
- `budget.duck` counts those units for the `duckdb` state of `budget_sessions`. [Rationale 5]
```

### `internal/olap/AGENTS.md`

The pool bullet (`:18-22`), "They do not count against
`pool.max_sessions`", becomes:

```markdown
- Idle handles are not counted, and `internal/qdb` counts each DuckDB call as one unit of `pool.max_sessions` (ADR-0017, Decision 7). [Rationale 1]
```

### `docs/e2e.md`

Stress definition (`:384-391`): "a budget of two sessions" becomes "a
budget of three sessions and a per-user cap of two". The budget step
paragraph (`:411-416`) is rewritten whole:

```markdown
- The budget step reads `e2e_stress_budget` from `STRESS_CLIENTS` native clients and as many `#sql` clients at once, through `xargs -P` and curl. [Rationale 7]
- While they read, it polls `qdb_rest_budget_sessions` and fails on a poll whose `in_use` exceeds `max`. [stress.sh:203-217]
- It fails when no poll saw a `duckdb` unit. [Rationale 7]
- Once the reads end, every client answered 200, a native body compares byte for byte with the generated rows, and a `#sql` body compares with them once decoded. [Rationale 7]
```

The scrape's `METRIC_SERIES` list (`:150-156`) is unchanged, because
it names series and `duckdb` is a state of `budget_sessions`.

### Tests

The unit adds no Go test (Rationale 8). The evidence is the stress's
budget step and the `duckdb` series. The existing Go tests and the round
trip's `#sql` steps run every DuckDB call through `withInstance` with
a unit taken, so a call that never got its unit back would show up
there as a wait.

## Rationale

| decision                                                                                              | why                                                                                                                  | rejected, and why                                                                                                                                                                                                                   | gained                                                         | given up                                                                                                                 | settled by        |
| ----------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | ----------------- |
| 1. A DuckDB call holds one budget unit while it runs                                                  | the server should know how many queries run through DuckDB, and each query can use at most one handle at a time      | a reservation of `max_pool_size` units per instance, which sized the budget to the worst case and needed the default pool size lowered below `max_sessions`; one unit per table reference, which needs the plan read before the run | the budget is again a ceiling on what DuckDB calls put on qdbd | idle handles and multi-table queries are under-counted                                                                   | owner, 2026-11-17 |
| 2. A call that finds the budget full waits, as a native dial does                                     | one unit per call has the shape of a native request, so it cannot deadlock, and one rule for both engines is simpler | answer 503 at once with `Retry-After`, which only the reservation needed                                                                                                                                                            | the budget code stays as it is                                 | a `#sql` call can wait behind idle native sessions until `pool.idle_timeout`                                             | owner, 2026-11-17 |
| 3. The unit is taken before the instance opens                                                        | the attach is a qdbd session like any other                                                                          | the unit taken only around the query, which leaves the first attach uncounted                                                                                                                                                       | the attach is counted                                          | a full instance cap answers 503 only after the wait for a unit                                                           | owner, 2026-11-25 |
| 4. A DuckDB describe holds a unit                                                                     | binding a table leases a handle, and a native describe holds a session                                               | a flag on `withInstance` that leaves the describe out                                                                                                                                                                               | one code path for both calls                                   | none                                                                                                                     | owner, 2026-11-25 |
| 5. `budget_sessions` gets a `duckdb` state, and `in_use` counts the DuckDB units too                  | the gauge shows the DuckDB part and keeps `in_use <= max` true                                                       | a separate gauge                                                                                                                                                                                                                    | the stress's existing assertion holds unchanged                | none                                                                                                                     | owner, 2026-11-17 |
| 6. The stress budget is three, the per-user cap two                                                   | one unit always stays free for DuckDB, and the step does not depend on the reaper's timing                           | a short `--pool-idle-timeout` with the budget at two                                                                                                                                                                                | the step proves `in_use <= max` with both engines reading      | none                                                                                                                     | owner, 2026-11-25 |
| 7. The budget step fails when no poll sees a DuckDB unit, and the sql clients number `STRESS_CLIENTS` | without it the step would pass if DuckDB calls were not counted                                                      | presence of the series only                                                                                                                                                                                                         | the step shows the count                                       | the step depends on a DuckDB read outlasting one poll interval of `poll_budget`, which a read of `STRESS_ROWS` rows does | proposal          |
| 8. No Go test                                                                                         | the owner's testing doctrine favours e2e evidence over low-value unit tests                                          | a unit test of `withInstance` waiting on a full budget                                                                                                                                                                              | no scaffolding                                                 | the wait is shown only by the stress, which runs by hand                                                                 | proposal          |
| 9. The unit does not touch the per-user cap                                                           | `pool.per_user_max` keeps meaning native sessions                                                                    | counting the DuckDB unit against the per-user cap                                                                                                                                                                                   | per-user semantics unchanged                                   | one user's DuckDB calls are bounded by `duckdb` governance and the global budget only                                    | owner, 2026-11-17 |
| 10. ADR-0017 Decision 7 is rewritten in place                                                         | the decision stays within ADR-0017's subject                                                                         | a new superseding ADR                                                                                                                                                                                                               | one place for the DuckDB decisions                             | bends the rule that an in-place edit keeps the decision unchanged                                                        | owner, 2026-11-17 |
| 11. The bindings bump stays out of this unit                                                          | the bump needs qdb-duck on DuckDB 2.0.1 first, and that upgrade runs in qdb-duck now                                 | the bump on this branch, which would block the merge                                                                                                                                                                                | this unit merges on its own                                    | story 20145 stays open                                                                                                   | owner, 2026-11-26 |

## Knowledge

1. Commit 2 (`budget.go`): the type comment and the inline notes at
   `duck`, `acquireDuck` and `releaseDuck`, from Rationale 1 and 5 and
   Verified 1.
2. Commit 3 (`cluster.go`): the `budget` field comment, the
   `withInstance` doc comment, overview and step notes, the `Duck` doc
   comment and the `DuckDescribe` body comment, from Rationale 1-4 and
   Verified 3, 4 and 6. Step 5's order is a proposal, and its reason
   lands inline.
3. Commit 4 (`collector.go`): the help string and step 1 of `Collect`,
   from Rationale 5.
4. Commit 5 (`examples/qdb_rest.yaml`): one operator-facing sentence on
   `max_sessions`, from Rationale 1.
5. Commit 6 (`Makefile`): the budget's reason, from Rationale 6 and
   Verified 8.
6. Commit 7 (`stress.sh`): the `sql_reads`, `poll_budget` and
   `budget_step` comments, from Rationale 7 and Verified 9.
7. Commit 8 (ADR-0017 Decision 7): Rationale 1-3, 9 and 10. Their
   rejected alternatives are written into the ADR's Alternatives
   rejected section (reservation, per-table-reference count, 503 at
   once).
8. Commit 9 (brief): the corrected scan sentence, from Verified 5, and
   Risks 5.
9. Commit 10 (`internal/AGENTS.md`) and commit 11
   (`internal/olap/AGENTS.md`): the folder rules, from Rationale 1-3 and 5.
10. Commit 12 (`docs/e2e.md`): the stress definition, from Rationale 6
    and 7.
11. Rationale 4 lands at `DuckDescribe`. Rationale 8 and 11 are
    `plan only`.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code and reconciles
the code against this plan before the build-stage message.

- Before commit 7, the executor checks that the sql clients' wait for a
  unit ends within the step under the budget of three. If they still
  wait up to `pool.idle_timeout`, the executor stops and asks the owner
  before it changes the step's shape.
- Commit 8 lands before commit 10 and commit 11, because both cite
  ADR-0017 Decision 7.

## Commits

1. `docs(plan): duckdb-query-budget-plan.md, duckdb queries count against the session budget`
2. `feat(qdb): the budget counts the units duckdb calls hold`
3. `feat(qdb): a duckdb call takes a budget unit before the instance open`
4. `feat(qdb): budget_sessions exposes the duckdb units as a series`
5. `docs(examples): pool.max_sessions says a sql call holds one session`
6. `test(e2e): the stress budget is three sessions`
7. `test(e2e): the stress budget step reads through #sql beside the native clients`
8. `docs(adr): adr-0017 decision 7 counts a duckdb call against the budget`
9. `docs(brief): embedded duckdb and risks 5 say a duckdb call takes a budget unit`
10. `docs(internal): a duckdb call takes one budget unit`
11. `docs(olap): qdb-duck's handles sit behind one budget unit per call`
12. `docs(e2e): the stress budget step names its sql clients and the duckdb units`
13. `/doc-discipline all internal/qdb tests/e2e`, committed as `docs(<scope>): ...` only if it changes anything.
14. `/doc-discipline check docs/duckdb-query-budget-plan.md`, fixed by further commits before commit 15.
15. `docs(log): duckdb-query-budget-plan.md deleted, facts moved to adr-0017, docs/brief.md, internal/AGENTS.md, internal/olap/AGENTS.md and docs/e2e.md`, which also deletes the log's two stale open items, the Flight stress case and the DuckDB budget.
16. Verify: push the story branch, open the pull request
    `QDB-20276 - [rest] DuckDB queries count against the session budget`,
    build its head in Buildkite, and wait for the result. Green: report
    the build number. Red: fix with further small commits on this
    branch, push, build again. Also run `make test-stress` by hand, because the
    stress is not in CI.

## Open questions and recommendations

1. Should the budget step fail when no poll sees a DuckDB unit
   (Rationale 7)? Recommendation: yes, because without it the step does
   not show the count.
2. Should the sql clients number `STRESS_CLIENTS` (Rationale 7)?
   Recommendation: yes, the same count as the native clients, with no
   new parameter.
3. Should step 5 of `withInstance` give the unit back before it
   uncounts the call? Recommendation: yes, so a pool's eviction never
   races a unit still held.
4. Should the reference config say that a sql call holds one session
   (commit 5)? Recommendation: yes, because an operator sizing
   `max_sessions` needs to know.
