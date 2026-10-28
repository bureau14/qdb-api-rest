# Plan: the duckdb block governs qdb-duck's tuning

Status: draft

## Outcome

When the unit lands, the `duckdb` config block carries six new keys:
`table_cache_ttl`, `max_pool_size`, `stabilization_max_wait`,
`transaction_max_wait`, `soft_memory_limit` and `load_balancing`. Every
DuckDB instance the server opens sets them as qdb-duck's `quasar_*`
globals on its attach connection before the `ATTACH`, together with the
cluster block's `timeout`, `max_in_buffer_size` and
`connections_per_address`, so the native sessions and qdb-duck's
sessions share one network policy. The table cache TTL and the pool
size are always sent, with qdb-duck's own defaults in `Default()`. The
other keys follow the cluster block's rule that zero leaves the C API
default and is not sent. Both test harnesses start their servers and
instances with the table cache disabled, which is the experiment on the
column-not-found flake the log's Handoff records. ADR-0017 Decision 7
names the keys and the path, and the brief's Risks item 5 keeps only
the budget question.

Left for later units: forwarding `cluster.compression`,
`cluster.encryption` and `cluster.parallelism` (Open questions 1 and
2); lending qdb-duck the session budget (the brief's Risks item 5);
qdb-duck PR #23, which this unit does not need.

## Verified facts

1. The DSN an instance opens with refuses every `quasar_*` option:
   `Invalid Input Error: The following options were not recognized:
quasar_table_cache_ttl`. Probe against `duck/` on 2026-10-28, in this
   session.
2. `SET GLOBAL quasar_<name> = <value>` on one connection of an open
   connector succeeds for all thirteen settings, and a second connection
   of the same connector reads the value back through
   `duckdb_settings()`. Probe on 2026-10-28 against the qdb-duck master
   package built 2026-10-28 08:17 UTC (macOS arm64, build
   `01a17fdc-4571-4a55-b6b8-2d235fa55d43`), which includes PR #20.
3. qdb-duck reads the globals once at `ATTACH`, then the ATTACH string,
   then the secret, and the secret never overwrites a key the ATTACH
   string set; the secret carries host, port and the three credential
   fields only. qdb-duck `src/extension/quasar_extension.cpp:113-152`,
   `src/extension/quasar_config.cpp:236-256`, both on master.
4. The thirteen settings, their DuckDB types and defaults: qdb-duck
   `src/extension/quasar_config.cpp:152-207` on master. `max_pool_size`
   1 to 1024, default 100; `table_cache_ttl` milliseconds, default
   60000, 0 disables; `timeout` milliseconds, at least 1000 when set,
   at most 3600000; `stabilization_max_wait` and `transaction_max_wait`
   milliseconds, 0 keeps the client default, must be at least `timeout`
   when both are set; `max_in_buf_size` and `soft_memory_limit` bytes,
   0 keeps the client default; `connection_per_address_soft_limit`, 0
   keeps the client default; `load_balancing` boolean, false.
   `ValidateQuasarConfig` at `quasar_config.cpp:258-305` holds the
   bounds.
5. qdb-duck applies every tuning option to each pooled handle when it
   connects, and a value the C API refuses fails the `ATTACH`. qdb-duck
   `src/extension/quasar_connection.cpp:24-98` on master.
6. The C API's table cache TTL call is experimental, defaults to 60 s,
   and 0 disables the cache: `qdb/include/qdb/option.h:618-630`.
7. The cluster block already applies `timeout`, `max_in_buffer_size`,
   `parallelism` and `connections_per_address` to the native sessions,
   each only when non-zero: `internal/qdb/cluster.go:125-155`. Its
   `timeout` is whole seconds, at least 1 s: `examples/qdb_rest.yaml:54-57`.
8. The config layer-fold property test enumerates every key by
   reflection and uses `validate` as its oracle, so a new field in
   `DuckDB` is covered by it without a new test:
   `internal/config/config_test.go:118-176`. The example file must equal
   `Default()`: `internal/config/config_test.go:271-277`.
9. `qdbtest.Cluster.Config` is the one copy of the test binding, and
   the cluster fixture's `Bind` builds from it:
   `internal/qdbtest/qdbtest.go:126-134`,
   `internal/qdbtest/cluster/cluster.go:29-33`. The olap tests open
   with `config.Default().DuckDB` today: `internal/olap/olap_test.go:50`.
10. The e2e harness starts each server with `REST_ARGS` and
    `REST_ARGS_SECURE`, flags in the form `--<block>-<key>`:
    `tests/e2e/Makefile:39-41,81-86`. CI calls `make test-round-trip`
    with the binary and no further arguments:
    `scripts/cicd/40.test-e2e.sh:139`.
11. CI fetches qdb-duck's master artifacts for every build of this
    repository: `.buildkite/AGENTS.md:37-42`. PR #20 merged 2026-10-28
    00:15 UTC and PR #23 is open, so the global path reaches every
    option in CI today and the ATTACH string does not.
12. `olap.Open` is called from one site, `internal/qdb/cluster.go:616`,
    on the `Cluster` that already holds the cluster block as `c.cfg`
    (`internal/qdb/cluster.go:45`) and the DuckDB block as `c.duckCfg`
    (`internal/qdb/cluster.go:47`).

## Design

### `internal/config/config.go`

```go
// DuckDB governs every DuckDB instance the server opens, one per user:
// the memory and worker threads one instance may take, where it may
// spill and how much, how many instances may be live at once, and the
// qdb-duck sessions behind one instance: how many handles the attached
// catalog pools, how long a handle caches table metadata, and the
// network and retry knobs the C API takes per handle [fact 4]
// The sizes are in bytes, and a wait of zero leaves the C API default
// where the help tag says so [fact 4, Rationale: zero rule]
type DuckDB struct {
	MemoryLimit          int64         `yaml:"memory_limit" help:"..."`
	Threads              int           `yaml:"threads" help:"..."`
	SpillDirectory       string        `yaml:"spill_directory" help:"..."`
	MaxSpillSize         int64         `yaml:"max_spill_size" help:"..."`
	MaxInstances         int           `yaml:"max_instances" help:"..."`
	MaxPoolSize          int           `yaml:"max_pool_size" help:"qdbd sessions qdb-duck pools behind one instance, 1 to 1024"`
	TableCacheTTL        time.Duration `yaml:"table_cache_ttl" help:"how long a qdb-duck session caches a table's metadata; 0 disables the cache"`
	StabilizationMaxWait time.Duration `yaml:"stabilization_max_wait" help:"longest wait for an unstable cluster per qdb-duck session; 0 means the C API default"`
	TransactionMaxWait   time.Duration `yaml:"transaction_max_wait" help:"longest retry of a conflicting transaction per qdb-duck session; 0 means the C API default"`
	SoftMemoryLimit      int64         `yaml:"soft_memory_limit" help:"client-side soft memory limit per qdb-duck session, in bytes; 0 means the C API default"`
	LoadBalancing        bool          `yaml:"load_balancing" help:"spread a qdb-duck session's requests across the nodes instead of pinning it to one"`
	// style: doc comment alone
	// because: a struct of tagged fields, the help tags are the contract
	//
	// at the two always-sent keys: the help text names no zero rule,
	// because 0 is a value for the TTL and refused for the pool size
	// [Rationale: zero rule]
}
```

```go
// Default returns the configuration used when nothing is specified
// (the existing list gains one line: qdb-duck's own pool of one hundred
// handles and one minute of table metadata cache, with the other knobs
// at the C API defaults) [fact 4]
func Default() Config {
	// style: doc comment alone + inline note at the DuckDB literal
	// because: a literal, and the existing DuckDB note already explains the governance values
	//
	// at DuckDB: the pool size and the TTL repeat qdb-duck's defaults
	// so that a server with no duckdb block behaves as before, and the
	// one minute is the C API's own default [fact 6, Rationale: defaults]
	// at DuckDB: the four other knobs stay zero, so nothing is sent and
	// the C API default holds, as the cluster block does [fact 7]
}
```

```go
// validateDuckDB requires a positive memory limit, at least one thread,
// a spill cap that is not negative, at least one instance, at least
// one pooled handle, and a TTL, two waits and a soft memory limit that
// are not negative [Rationale: bounds]
func validateDuckDB(d DuckDB) error {
	// style: doc comment alone + inline note at the pool size check
	// because: one guard per key, as the existing checks are
	//
	// at MaxPoolSize: only the lower bound is here, because the 1024
	// cap, the millisecond caps and the "wait at least timeout" rule
	// are qdb-duck's and it judges them at the attach, and the
	// validation rule keeps one home per check [internal/AGENTS.md, Code; fact 4]
}
```

### `examples/qdb_rest.yaml`

```yaml
duckdb:
  # (existing keys unchanged)
  # qdbd sessions qdb-duck pools behind one instance, 1 to 1024. A scan
  # takes one per DuckDB worker, and a catalog lookup can hold several.
  max_pool_size: 100
  # How long a qdb-duck session caches a table's metadata. 0 disables
  # the cache, so every lookup asks qdbd.
  table_cache_ttl: "1m"
  # Per qdb-duck session, with 0 as the C API default: the longest wait
  # for an unstable cluster and the longest retry of a conflicting
  # transaction, each at least cluster.timeout when both are set, and
  # the client-side soft memory limit in bytes.
  stabilization_max_wait: "0s"
  transaction_max_wait: "0s"
  soft_memory_limit: 0
  # Spread a session's requests across the nodes instead of pinning it
  # to one.
  load_balancing: false
```

Claims: the pool bullet's "one per DuckDB worker" is the brief's
(`docs/brief.md:507-511`), the lookups claim is qdb-duck's option
description [fact 4], the "at least cluster.timeout" is qdb-duck's
validation [fact 4].

### `internal/olap/instance.go`

```go
// settingsOf renders the qdb-duck globals an instance sets before its
// attach, one SET GLOBAL statement per setting the configuration
// gives: the duckdb block's own knobs and the cluster block's network
// knobs [Rationale: keys; fact 2]
// A statement carries a number or a boolean the server renders, never
// a user's text [Rationale: mechanism]
func settingsOf(duck config.DuckDB, cluster config.Cluster) []string {
	// style: doc comment + numbered overview + step comments
	// because: the function encodes which keys are always sent and which only when set, and the unit conversions
	//
	// The globals are read once at the ATTACH and the instance serves
	// one user, so a global is this user's setting [fact 3; ADR-0017 Decision 5]
	//
	// 1. the pool size and the TTL are always sent, because qdb-duck
	//    reads a TTL of 0 as "no cache" and the config default repeats
	//    qdb-duck's own, so sending them unconditionally keeps one rule
	//    [fact 4, Rationale: zero rule]
	//    - qdb-duck takes milliseconds, so the duration is divided
	//      [fact 4]
	// 2. the waits, the buffer cap, the soft memory limit and the
	//    connections per address are sent only when non-zero, because 0
	//    is "keep the client default" on both sides and sending 0 says
	//    the same thing in more statements [fact 4, fact 7]
	//    - cluster.timeout, cluster.max_in_buffer_size and
	//      cluster.connections_per_address come from the cluster block
	//      so both session kinds see one network policy; compression,
	//      encryption and parallelism stay out [Rationale: forwarded knobs]
	// 3. load balancing is sent only when true, because false is
	//    qdb-duck's default [fact 4]
}
```

```go
// secretOf is the CREATE SECRET statement that holds the credentials.
func secretOf(c Credentials) string {
	// style: doc comment + overview (existing, rewritten whole)
	// because: the existing overview records the rejected alternatives, and one claim changes
	//
	// The credentials travel as quoted literals of a secret; the ATTACH
	// string was rejected because qdb-duck splits it on every = and a
	// key ends in base64 padding [existing comment, unchanged]
	// The DSN was rejected for qdb-duck's options, because DuckDB
	// refuses them at open; the tuning travels as globals instead
	// (settingsOf) [fact 1, fact 2]
	// Only the host, the port and the credentials travel in the secret;
	// the cluster's compression and encryption are not forwarded,
	// because qdb-duck spells their values differently (fast, best,
	// balanced; aes_gcm_256, aegis_256) and no mapping of the REST
	// vocabulary is settled [fact 4, Open question 2]
	// The secret is TEMPORARY, so it lives in memory with the instance
	// and never reaches disk [existing comment, unchanged]
}
```

```go
// Open opens an instance under the two configuration blocks and
// attaches the cluster as qdb with the credentials. A failed attach is
// ErrAttach with qdb-duck's message, and leaves no instance open; a
// setting the C API refuses fails the attach the same way [fact 5]
func Open(ctx context.Context, duck config.DuckDB, cluster config.Cluster, c Credentials) (*Instance, error) {
	// style: doc comment + numbered overview + step comments (existing, step 2 widened)
	// because: three steps with one failure path that must undo the first
	//
	//  1. open the connector, where DuckDB judges the configuration [existing]
	//  2. on one connection, set the globals, create the secret and
	//     attach through it; the globals go first because the attach
	//     reads them once [fact 3]
	//  3. on a failed attach, close the connector and answer ErrAttach [existing]
}
```

```go
// attach sets the globals, creates the secret and attaches the cluster
// as qdb through it, on one connection of the connector. A failed
// attach is ErrAttach with qdb-duck's message.
func attach(ctx context.Context, connector *duckdb.Connector, settings []string, c Credentials) error {
	// style: doc comment alone + inline note at the settings loop
	// because: a sequence of three statements whose order the doc comment states
	//
	// at the loop: a refused SET is a config error and not an attach
	// error, so it returns unchanged, as the secret's does; only the
	// ATTACH wraps ErrAttach, because only it dials [existing attach
	// comment; internal/qdb/cluster.go:620-640 classifies ErrAttach]
}
```

### `internal/qdb/cluster.go:616`

```go
	// one-line change at the open site: olap.Open(ctx, c.duckCfg, c.cfg, creds)
	// style: no new comment
	// because: the call forwards the two blocks the Cluster already holds [fact 12]
```

### `internal/qdbtest/qdbtest.go`

```go
// Config returns the default configuration bound to the cluster (the
// existing contract), with the qdb-duck table cache disabled [Rationale: test default]
func (c Cluster) Config(t testing.TB) config.Config {
	// style: doc comment alone + inline note at the TTL line
	// because: assignments, and one of them carries a decision
	//
	// at TableCacheTTL = 0: every test instance asks qdbd for table
	// metadata on each lookup, because the column-not-found and
	// handle-is-invalid flakes on the #sql path are suspected to be a
	// pooled handle with stale metadata, and a CI run without the cache
	// is the experiment; the shipped default stays one minute
	// [docs/log.md Handoff; owner, 2026-10-28]
}
```

### `internal/olap/olap_test.go`

```go
// open opens an instance with the fixture's configuration and the
// credentials, and closes it when the test ends.
func open(t *testing.T, f cluster.Fixture, c olap.Credentials) *olap.Instance {
	// style: doc comment alone
	// because: a helper that assigns and registers a cleanup
	//
	// cfg := f.Spec.Config(t); olap.Open(ctx, cfg.DuckDB, cfg.Cluster, c)
	// so the olap tests open with the test TTL from the one copy [fact 9]
}
```

### `tests/e2e/Makefile`

```make
# REST_ARGS and REST_ARGS_SECURE each gain:
#                 --duckdb-table-cache-ttl 0s
# with one comment above REST_ARGS:
# Both servers run qdb-duck without its table metadata cache, so every
# DuckDB lookup asks qdbd. The round trip's sql step has failed with
# "The timeseries does not contain this column" from a pooled handle
# that cached a table's metadata before the table changed, and this
# is the experiment on it; the shipped default is one minute.
# [docs/log.md Handoff; owner, 2026-10-28]
```

### Documents

`docs/adr/0017-embedded-duckdb.md`, Decision 7, rewritten whole:

```
- qdb-duck's handle pool is sized and tuned by the `duckdb` block and the
  cluster block's network knobs, set as qdb-duck's globals on the attach
  connection before the ATTACH (owner, 2026-10-28). Its sessions still
  do not count against pool.max_sessions, and idle handles live until
  detach. The brief's Risks item keeps the budget question.
```

Alternatives table gains one row: "ATTACH string parameters for the
tuning: pool size and TTL need qdb-duck PR #23 and a refreshed artifact
on every platform, and the globals reach every option today". The
existing row "An upstream pool size parameter before M4" stays.

`docs/brief.md`, Risks item 5, rewritten whole: the pool opens sessions
outside the budget and never closes an idle one before detach; the
attach passes the pool size and the TTL (Embedded DuckDB); what stays
open is the budget lending qdb-duck its sessions, looked at after the
milestones (owner, 2026-10-13). The Embedded DuckDB section's "qdb-duck
pools its own qdbd sessions" paragraph says the pool size is
`duckdb.max_pool_size`, one hundred by default.

`docs/log.md`: the Handoff flake item gains one sentence, that both
harnesses run with the table cache disabled since this unit, and the
builds that follow are the experiment. One entry:
`duckdb block governs qdb-duck's tuning; ADR-0017 Decision 7 edited in place (owner, 2026-10-28)`.

`internal/AGENTS.md`, the DuckDB-call bullet: one sentence, that the
instance opens with the `duckdb` block's pool size, TTL and knobs and
the cluster block's network knobs as qdb-duck globals (`settingsOf`).

`internal/olap/AGENTS.md`, the pool bullet rewritten whole: qdb-duck
pools up to `duckdb.max_pool_size` sessions per attached catalog, one
hundred by default, and they live until the instance closes; the
settings reach qdb-duck as globals on the attach connection, and
`settingsOf` says which keys and why.

`internal/config/` has no AGENTS.md; the validation rule in
`internal/AGENTS.md` already names `validateDuckDB`.

## Rationale

| decision                                                                                                                                                                                                                                                | why                                                                                                                                                                                                                                   | rejected, and why                                                                                                                | gained                                                                                                            | given up                                                                                                                                                      | settled by                                       |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| keys: `table_cache_ttl`, `max_pool_size`, `stabilization_max_wait`, `transaction_max_wait`, `soft_memory_limit`, `load_balancing` under `duckdb`, plus forwarding of `cluster.timeout`, `cluster.max_in_buffer_size`, `cluster.connections_per_address` | the owner wants the tunables exposed and test defaults; the TTL is the flake experiment; the pool size is the brief's Risks item; both session kinds should share one network policy                                                  | TTL and pool size alone: leaves every PR #20 knob unreachable; a `duckdb.quasar.*` sub-block: one more level for six keys        | one block governs an instance end to end                                                                          | six more keys to document and keep in step with qdb-duck                                                                                                      | owner, 2026-10-28                                |
| mechanism: `SET GLOBAL quasar_*` on the attach connection before the `ATTACH`                                                                                                                                                                           | the DSN refuses the options (fact 1), the globals work for all thirteen (fact 2), one instance is one user so a global is per user (ADR-0017 Decision 5)                                                                              | ATTACH string parameters: pool size and TTL need PR #23 and a refreshed artifact on every platform; the DSN: refused at open     | works with the package in the tree and in CI today, one path for every option                                     | the attach runs up to twelve statements before the ATTACH; a per-catalog setting is not distinguishable from a global, which does not matter with one catalog | owner, 2026-10-28                                |
| zero rule: TTL and pool size always sent; other knobs sent only when non-zero                                                                                                                                                                           | qdb-duck reads TTL 0 as "no cache", so 0 cannot mean "default" for it; the pool size has a lower bound of 1 so 0 is refused anyway; the rest follow the cluster block's existing rule (fact 7)                                        | a sentinel (-1) for "leave default" on the TTL: a third meaning for one key, against the plain-option preference                 | one rule per key kind, each stated in its help tag                                                                | the config default must repeat qdb-duck's defaults and move with them                                                                                         | owner, 2026-10-28 (TTL); proposal (rest)         |
| defaults: pool 100, TTL 1m, rest 0, load balancing false                                                                                                                                                                                                | a server with no `duckdb` block behaves as before the unit, and qdb-duck's defaults are the ones its tests run under                                                                                                                  | a smaller pool tied to `duckdb.threads`: the brief's Risks item asks for the budget question later, not now                      | no behaviour change for existing deployments                                                                      | the shipped default does not disable the cache; the experiment is tests only                                                                                  | owner, 2026-10-28                                |
| bounds: config refuses pool size below 1 and negative TTL, waits and memory limit; qdb-duck judges the 1024 cap, the millisecond caps and "wait at least timeout"                                                                                       | validation has one home and config owns the simple bounds only (`internal/AGENTS.md`, Code); qdb-duck already holds the rest (fact 4)                                                                                                 | copying qdb-duck's bounds into config: a second layer to keep in step with qdb-duck                                              | startup refuses what cannot serve and names the key; the rest fails the first `#sql` call with qdb-duck's message | an out-of-range wait is found at the first DuckDB call and not at startup                                                                                     | proposal                                         |
| test default: TTL 0 in `qdbtest.Cluster.Config` and in the e2e `REST_ARGS`, shipped default unchanged                                                                                                                                                   | the column-not-found flake came from the e2e round trip on Windows and the handle-is-invalid one from the Go tests on macOS (`docs/log.md` Handoff), so both harnesses run the experiment                                             | shipped default 0: every lookup hits qdbd on real workloads; e2e only: misses the macOS flake                                    | CI's builds after this unit measure the flake rate without the cache                                              | the tests no longer run the shipped TTL, so a cache bug would show only in production                                                                         | owner, 2026-10-28                                |
| shape: `Open` takes the cluster block, `settingsOf(duck, cluster)` renders the statements                                                                                                                                                               | the Cluster already holds both blocks (fact 12), and olap already imports config                                                                                                                                                      | a settings struct copied field by field in `internal/qdb`: a third copy of the key list                                          | the key list lives once, in `settingsOf`                                                                          | `Open`'s signature widens by one block                                                                                                                        | proposal                                         |
| not forwarded: `cluster.parallelism`, `cluster.compression`, `cluster.encryption`                                                                                                                                                                       | qdb-duck's threads is per handle with default 1 and `duckdb.threads` already bounds a scan's sessions (ADR-0017 consequences); compression and encryption vocabularies differ (fact 4) and the REST `aes` has no verified counterpart | forwarding parallelism: multiplies sessions per scan by the per-handle count; mapping `aes`: a guess between two qdb-duck values | no guessed mapping ships                                                                                          | the two session kinds can differ in compression and encryption                                                                                                | proposal; Open questions 1 and 2                 |
| record: ADR-0017 Decision 7 edited in place, Risks item 5 shrunk, one log entry                                                                                                                                                                         | `docs/AGENTS.md` allows an in-place edit for a decision within an accepted ADR's subject, and this unit brings Decision 7's "after the milestones" forward                                                                            | a new ADR: more to read for a decision inside ADR-0017's subject                                                                 | one ADR keeps the DuckDB decisions                                                                                | none                                                                                                                                                          | owner, 2026-10-28                                |
| qdb-duck PR #23 is not a dependency                                                                                                                                                                                                                     | the globals path does not need it (fact 2)                                                                                                                                                                                            | waiting for #23: delays the experiment for no gain on this path                                                                  | the unit lands on the artifact CI already fetches                                                                 | none                                                                                                                                                          | owner, 2026-10-28 (through the mechanism choice) |

## Knowledge

Commit 2 (config keys): the `DuckDB` type comment and help tags carry
the key meanings [fact 4]; `Default`'s inline note carries the defaults
row; `validateDuckDB`'s doc comment and inline note carry the bounds
row and the one-home rule.

Commit 3 (example file): the example's comments carry the per-key
help in operator words, the "at least cluster.timeout" rule and the
"one per DuckDB worker" claim.

Commit 4 (settingsOf and attach): `settingsOf`'s overview carries the
mechanism row, the zero rule row, the forwarded-knobs row and facts 2,
3 and 4; `secretOf`'s rewritten overview carries fact 1 and the
not-forwarded row; `Open`'s overview carries fact 3's ordering;
`attach`'s inline note carries the error classification.

Commit 5 (cluster call site): nothing new to say.

Commit 6 (test defaults): `qdbtest.Cluster.Config`'s inline note and
the Makefile comment carry the test-default row and the Handoff's flake
facts; `olap_test.open`'s doc comment carries fact 9.

Commit 7 (documents): ADR-0017 Decision 7 and its alternatives row
carry the mechanism row and the record row; the brief's Risks item 5
and the Embedded DuckDB paragraph carry what stays open; the log's
Handoff sentence and entry carry the experiment and the owner decision;
`internal/AGENTS.md` and `internal/olap/AGENTS.md` point at
`settingsOf`.

Every Rationale row lands in at least one of the above. The PR #23 row
lands in ADR-0017's alternatives table.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from the Design and Knowledge sections in the same commit as the
code and reconciles the code against this plan before the build-stage
message. Specific to this unit:

- Before commit 4 the executor confirms fact 2 against the `duck/`
  package on the build machine, because a developer `duck/` older than
  2026-10-28 refuses the six PR #20 names at `SET GLOBAL`; the fix is
  the latest master artifact, not a code change.
- Commit 7's log entry and Handoff sentence land after commits 4 and 6,
  because they describe what those commits made true.
- If `SET GLOBAL` of a duration or a size needs a cast the probe did not
  exercise (the probe set integer literals and one boolean), the
  rendering in `settingsOf` is the integer millisecond or byte literal
  and nothing else; the plan does not authorise a `::UBIGINT` cast
  without the executor verifying the refusal first.

## Commits

1. `docs(plan): duck-tuning-plan.md, the duckdb block governs qdb-duck's pool, table cache and network knobs as globals before the attach` (this document)
2. `feat(config): duckdb.max_pool_size, table_cache_ttl, stabilization_max_wait, transaction_max_wait, soft_memory_limit and load_balancing, qdb-duck's defaults, refused when negative or an empty pool`
3. `docs(config): examples/qdb_rest.yaml carries the six keys at their defaults`
4. `feat(olap): settingsOf renders the quasar_* globals and attach sets them before the ATTACH, with the cluster's network knobs forwarded`
5. `feat(qdb): Cluster hands olap.Open the cluster block`
6. `test(qdbtest): Config disables the qdb-duck table cache, the olap tests open with it, and REST_ARGS start both e2e servers without it`
7. `docs(adr): ADR-0017 Decision 7 names the duckdb keys and the global path, the brief's Risks item keeps the budget question, and the AGENTS.md bullets point at settingsOf`
8. `/doc-discipline all internal/config internal/olap internal/qdb internal/qdbtest tests/e2e docs/duck-tuning-plan.md`, with any findings as further small commits
9. `docs(log): duck-tuning-plan.md deleted, facts moved to ADR-0017, docs/brief.md, internal/AGENTS.md, internal/olap/AGENTS.md and the comments of internal/config, internal/olap, internal/qdbtest and tests/e2e`
10. Verify: push `sc-19567/rr-duck-tuning`, build its head in Buildkite, wait for the result. Green: report the build number, and read the sql steps of every platform for the two flake signatures. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. Whether `cluster.parallelism` should reach qdb-duck as
   `quasar_threads`. Recommendation: no, because `duckdb.threads`
   already bounds a scan's sessions and qdb-duck's per-handle default is
   one.
2. Whether `cluster.compression` and `cluster.encryption` should be
   mapped and forwarded. Recommendation: not in this unit; `none` and
   `balanced` map directly, and the REST `aes` needs the owner to name
   `aes_gcm_256` or `aegis_256` before a mapping ships.
3. Whether the log's Handoff flake item is kept with the experiment
   sentence or deleted once the test default lands. Recommendation:
   kept, until the builds after this unit show the flake gone or still
   there.
