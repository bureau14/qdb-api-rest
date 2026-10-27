# DuckDB 2.0.0: the binding and the package move together -- Plan

Status: approved

## Outcome

When this unit lands:

- The binary links the DuckDB 2.0.0 line of the duckdb-go binding
  (`v2.20000.0`, bindings `v0.20000.0`) against qdb-duck's 2.0.0
  package on all eight platforms, and `olap.Version()` reports 2.0.0 on
  a build of the base.
- On Linux and FreeBSD, where the static bundle is linked, the quasar
  extension is registered before the first database opens, because the
  binding's package init calls the registration function the bundle
  carries. `CREATE TEMPORARY SECRET ... (TYPE quasar, ...)` succeeds,
  and the `#sql` step of the e2e round trip is green there.
- `vendor/` holds the 2.0.0 binding, and the five stub modules under
  `third_party/duckdb-nolib` stand in for the prebuilt library modules
  under their new module path, `go.duckdb.org/libduckdb/<os>-<arch>`,
  so a re-vendor commits no archive.
- `TestDescribe` passes against the 2.0.0 library, and the describe map
  carries whatever case the test forced.
- ADR-0017, `third_party/AGENTS.md`, the olap package comment, the
  describe conversion comment and the `.envrc` bundle comment say what
  is true of 2.0.0, with no mention of what came before.
- `docs/log.md` Current state names the held readiness-drain branch as
  in flight and says what its merge waits on.

Left for later: the rebase of `sc-19567/rr-readiness-drain` onto the
base after this unit merges, dropping its two artifact pins of
`.buildkite/pipeline.py` and their log commits (owner's call, Open
questions); any symbol read that answers `(unknown)` on current qdbd,
which is qdb-duck's defect and not this repository's; the systemd
notify protocol and the stress's drain step (`docs/log.md`, Next).

## Verified facts

- The `#sql` path fails on qdb-duck's 2.0.0 package with
  `Secret type "quasar" not found` on every query; the held branch's
  pipeline comment and log record it, and the base knows nothing of it
  (`sc-19567/rr-readiness-drain`, `.buildkite/pipeline.py:174-184`;
  the base's `.buildkite/pipeline.py` has no `build_id` entry)
  (2026-10-27).
- DuckDB 2.0.0 registers a statically linked extension only when the
  program calls a generated loader: `extension_build_tools.cmake`
  emits `<TARGET>_static_extension_loader.c` defining
  `duckdb_register_static_extensions()`, which passes every linked
  extension's describe function to `duckdb_register_static_extension`
  (qdb-duck `duckdb/extension/extension_build_tools.cmake:21,40`,
  `duckdb/extension/README.md:274`,
  `duckdb/src/include/duckdb_static_extension.h`, at submodule
  `d54cbbe3`, tag v2.0.0).
- qdb-duck master (`21faaffe1`, PR #19, "Upgrade to DuckDB 2.0.0")
  builds that loader as `duckdb_bundle_extension_loader` with the
  symbol exported, folds its archive into `lib/libduckdb_bundle.a` on
  Linux and FreeBSD, and ships `include/duckdb_static_extension.h`;
  its CMake comment states the contract: the embedding consumer of the
  static bundle calls `duckdb_register_static_extensions()` before it
  opens a database, while the DuckDB shared library and executables
  compile their own registration (qdb-duck `CMakeLists.txt`, the
  `duckdb_bundle_extension_loader` block; `scripts/cicd/40.package.sh`,
  `bundle_duckdb_static` and the `include/` copy).
- The 1.5.4 binding this repository vendors never calls that function:
  its static-lib file sets the include path and the static macro and
  nothing else
  (`vendor/github.com/duckdb/duckdb-go-bindings/cgo_static.go:1-6`).
  That is the whole cause of the failure.
- The 2.0.0 binding calls it itself. Under every tag except
  `duckdb_use_lib`, `cgo.go` declares
  `int32_t duckdb_register_static_extensions(void);` and calls it from
  `func init()`, with the comment that DuckDB reports a failed
  registration when a database opens
  (`/Users/lmergen/pkg/mod/github.com/duckdb/duckdb-go-bindings@v0.20000.0/cgo.go:1-22`).
  The dynamic file is unchanged and has no init
  (`.../cgo_dynamic.go:1-8`), so macOS and Windows take the dylib's and
  the DLL's own registration. Registering twice is a no-op
  (`.../README.md:95-105`).
- The 2.0.0 binding requires the prebuilt library modules under
  `go.duckdb.org/libduckdb/{darwin-amd64,darwin-arm64,linux-amd64,linux-arm64,windows-amd64}`
  at `v0.20000.0`, imported only when neither link tag is set
  (`.../duckdb-go-bindings@v0.20000.0/go.mod:6-10`,
  `prebuilt_*.go:1`). The repository's replace lines name the old
  paths `github.com/duckdb/duckdb-go-bindings/lib/<os>-<arch>`
  (`go.mod:76-84`), and each stub's module line names the same old
  path (`third_party/duckdb-nolib/<os>-<arch>/go.mod:1`).
- The exported API of package `duckdb` that olap uses is unchanged
  between the versions: `NewConnector` (`duckdb.go:60`),
  `Connector.Connect` and `Close`, `NewArrowFromConn` and
  `Arrow.QueryContext` (`arrow.go:84-118`, still under
  `//go:build duckdb_arrow`), `Stmt.ColumnCount`, `ColumnName`,
  `ColumnType`, `ColumnTypeInfo`, `NumInput`, `TypeInfo`, the detail
  structs, `Error`, and the 39 `TYPE_*` constants (`type.go` in both)
  (2026-10-27). olap uses 53 identifiers of the package, all present
  (`internal/olap/instance.go`, `describe.go`, `olap.go`).
- DuckDB 2.0.0 adds the C type `DUCKDB_TYPE_TIMESTAMP_TZ_NS = 42`
  (qdb-duck `duckdb/src/include/duckdb.h:287`), and the bindings
  expose it as `TypeTimestampTZNS` (`.../enums.go:57`), but package
  `duckdb` exports no `TYPE_` constant for it. `arrowTypeOf` has no
  case for it, so a column of that type is `ErrUndescribable` through
  `typeName`'s default branch (`internal/olap/describe.go:36-51`).
- The new binding's module requirements are below the repository's:
  Go 1.24 against `go 1.27`, arrow-go 18.5.1 and 18.4.1 against
  18.8.0 (`go.mod:3,9`), so the bump pulls no other upgrade.
- The binding's own header is found after the package's: the bindings'
  `#cgo CPPFLAGS: -I${SRCDIR}/include` follows `CGO_CFLAGS`, which
  `.envrc` sets to `-I${DUCK_INCLUDE_DIR}` first
  (`.envrc:71,77,83,122`), so the package's `duckdb.h` is the one
  compiled against on every platform. Today both are 1.5.4; after the
  bump the binding's is 2.0.0 and `duck/` must be too.
- The local `duck/` is the 1.5.4 macOS dylib of 2026-10-13
  (`duck/lib/libduckdb.dylib` reports `v1.5.4`; no
  `duckdb_static_extension.h` in `duck/include`). No local test can run
  under the bumped binding until a 2.0.0 package replaces it.
- The only DuckDB open in the binary is `duckdb.NewConnector` in
  `olap.Open` (`internal/olap/instance.go:124`), reached lazily through
  `Cluster.instanceOf` on a user's first DuckDB call
  (`internal/qdb/cluster.go:587-616`) and through the test helper
  `open` (`internal/olap/olap_test.go:48-60`). Nothing opens DuckDB
  before it, and `--version` prints `olap.Version()` without an open
  (`cmd/qdb_rest/main.go:47,216-220`).
- `TestDescribe` compares `Describe`'s schema with the schema the Arrow
  reader answers for the same expression, on both clusters, and pins
  the refusals (`internal/olap/olap_test.go:158-228`). `TestDuckDBScalars`
  pins the typing of literals and casts through the HTTP path
  (`internal/httpapi/duckdb_test.go:22-82`), and the e2e types query
  must match it (`tests/e2e/round-trip.sh:281-288`). These three are
  the guards a conversion change trips.
- The vendoring constraint: a `vendor/` that disagrees with `go.mod`
  fails every `go` command with inconsistent vendoring, and a replace
  directory whose `go.mod` names a module other than the replaced one
  fails `go mod` with a module path mismatch. So the version bump, the
  stub repoint and the re-vendor are one commit (Go's module and vendor
  rules, 2026-10-27).
- The re-vendor sequence this repository uses is `go get`, `go mod
tidy`, `go mod vendor` (`internal/AGENTS.md:24-27`), and bumping the
  binding bumps `go.mod` only, the stubs carrying no version
  (`third_party/AGENTS.md:28-29`). The module path change is the one
  case where the stubs do change.
- `make lint` under this machine's Go 1.27.3 fails on export data; the
  build stage runs it as `GOTOOLCHAIN=go1.27.0 make lint` (memory
  `lint-toolchain-go1270`, to be verified by running it).
- The base's `docs/log.md` says In flight: Nothing and lists the
  stress's drain step as its only Next item
  (`docs/log.md`, Current state, base `aeb1def`); the held branch's log
  names itself in flight and the qdb-duck unit as Next 1. Those lines
  reach the base with the held branch's rebase, not with this unit.

## Design

In file order. The sketches carry the claims and their evidence; the
build writes the prose.

### `go.mod`

```
require github.com/duckdb/duckdb-go/v2 v2.20000.0
// indirect: github.com/duckdb/duckdb-go-bindings v0.20000.0
// indirect: go.duckdb.org/libduckdb/<os>-<arch> v0.20000.0, five lines

replace go.duckdb.org/libduckdb/darwin-amd64 => ./third_party/duckdb-nolib/darwin-amd64
// ... the five replace lines, one per prebuilt module, the old five removed
// [bindings@v0.20000.0/go.mod:6-10; third_party/AGENTS.md:19-24]
```

`go.sum` and `vendor/` follow from `go mod tidy` and `go mod vendor`.

### `third_party/duckdb-nolib/<os>-<arch>/go.mod` and `lib.go` (five)

```
module go.duckdb.org/libduckdb/<os>-<arch>

go 1.27
```

```go
// Package lib is the stub that stands in for the prebuilt <os>-<arch>
// DuckDB library module the binding imports when no link tag is set.
// [bindings@v0.20000.0/prebuilt_<os>_<arch>.go:1]
// Why it exists and what it must not carry is in third_party/AGENTS.md.
package lib
```

### `third_party/AGENTS.md`, section `duckdb-nolib`

```
- the five modules are go.duckdb.org/libduckdb/{darwin-amd64, darwin-arm64,
  linux-amd64, linux-arm64, windows-amd64}, the prebuilt DuckDB library
  modules of duckdb-go-bindings [bindings@v0.20000.0/go.mod:6-10]
- the binding imports a prebuilt module only when neither link tag is
  set, and .envrc sets one on every platform [unchanged claim, .envrc:73-124]
- a bare go mod vendor would commit the archives without the
  replacement [unchanged claim, owner 2026-10-14]
- a stub carries no cgo directive and no symbol; a build without the
  tags fails at link time, the signal .envrc was not sourced [unchanged]
- bumping the binding bumps go.mod only, except when the binding moves
  the modules to a new path, when each stub's module line follows it
  [this unit, Rationale: stubs repointed]
```

### `internal/olap/olap.go`, package comment

```go
// Package olap embeds DuckDB with the quasar extension ... [unchanged lead]
// The library comes from qdb-duck's package in duck/, and the root
// .envrc locates it and selects the binding's build tags per OS
// (ADR-0017). [unchanged]
// On the platforms that link the static bundle, the binding registers the
// bundle's linked extensions from its package init, before any database
// opens, so the quasar secret type and catalog exist on the first
// connection; the shared library on the other platforms registers its
// own. [bindings@v0.20000.0/cgo.go:18-22; qdb-duck CMakeLists.txt,
// duckdb_bundle_extension_loader; Rationale: no registration call here]
// The package opens one instance per user ... [unchanged]
package olap
```

`Version()` is unchanged.

### `internal/olap/describe.go`, `arrowTypeOf`

The doc comment is unchanged. The overview's first paragraph is
rewritten:

```go
func arrowTypeOf(t duckdb.TypeInfo, tz string) (arrow.DataType, error) {
	// style: doc comment + overview + step comments (existing shape kept)
	// because: a hand copy of DuckDB's conversion, and the reader must
	// know which release and which client properties it copies
	//
	// The cases follow SetArrowFormat in arrow_converter.cpp of DuckDB
	// 2.0.0, the release qdb-duck's submodule names, under its default
	// client properties [qdb-duck duckdb at tag v2.0.0,
	// src/common/arrow/arrow_converter.cpp; the property values are
	// re-read there at build time and written only as found]
	// the two rejected alternatives stay as they are [describe.go:77-80]
	//
	// at any case TestDescribe fails: the case follows what the reader
	// answers for that expression, and the comment at the case says
	// which 2.0.0 conversion rule it follows [olap_test.go:158-228]
}
```

The decimal claim at `describe.go:107-108` ("output version 1.0 is
below 1.5, where DuckDB starts to narrow a decimal") is re-read against
2.0.0's `arrow_converter.cpp` at build time: kept if the rule and the
default output version are the same there, rewritten from the source
if not. No claim is written from memory.

### `.envrc`, the `duck/` comment block

```
# duck/ holds qdb-duck's package in the layout of the C API package:
# include/duckdb.h and include/duckdb_static_extension.h everywhere,
# lib/libduckdb_bundle.a on Linux and FreeBSD (every DuckDB static
# library, the quasar extension and the loader that registers the
# linked extensions, in one archive without the qdb_api objects),
# lib/libduckdb.dylib on macOS, and bin/duckdb.dll with lib/duckdb.lib
# on Windows (ADR-0017). [qdb-duck scripts/cicd/40.package.sh at 21faaffe1]
```

The tag paragraph (`.envrc:43-52`) gains one sentence after "defines
the static export macro": under that tag the binding also calls the
bundle's registration function from its init, so the bundle must define
it, and a bundle without it fails the link with an undefined symbol
[bindings@v0.20000.0/cgo.go:12,18-22; README.md:95-105]. The rest of
the block is unchanged.

### `docs/adr/0017-embedded-duckdb.md`

Decision 2 gains, after the sentence on the tags: "Under
`duckdb_use_static_lib` the binding registers the bundle's linked
extensions from its package init before any database opens, so the
bundle carries DuckDB's generated loader; the dynamic libraries
register their own." Decision 3 reads: "The DuckDB release is pinned
in pairs. The qdb-duck DuckDB submodule and the go-duckdb tag name the
same release. Today that is 2.0.0, so go-duckdb is taken from the tag
line that bundles it, and the next bump moves both." The alternatives
table keeps its row on bumping to the newest go-duckdb release, and
gains one:

```
| A registration call of this repository's own in internal/olap | the binding's init already makes it under the static-lib tag, and a second call is a no-op that would have to be kept in step with the binding |
```

### `docs/log.md`, Current state

```
In flight:

- `sc-19567/rr-readiness-drain`: readiness answers 503 without a dial
  for the whole drain, built and reviewed, held before its merge
  (owner, 2026-10-27). It rebases onto the base once this unit has
  merged and merges once a build of its head is green.

Blocked on:

- The merge of `sc-19567/rr-readiness-drain` waits on its rebase and a
  green build of its head.
```

The dated entry: `duckdb-2-plan.md deleted; facts moved to ADR-0017
(Decisions 2 and 3, Alternatives), third_party/AGENTS.md, and the
comments of internal/olap and .envrc`.

## Rationale

| decision                                                                                                                | why                                                                                                                                                      | rejected, and why                                                                                                                        | gained                                         | given up                                                                                            | settled by                                                                           |
| ----------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- | --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| The fix is the binding bump; no registration call in `internal/olap`                                                    | the 2.0.0 binding's init calls `duckdb_register_static_extensions()` under the static-lib tag, and a second call is a no-op                              | a cgo call in `olap.Open` under a build tag: a second copy of the binding's own step, kept in step by hand                               | one fewer cgo file, no build-tagged Go in olap | nothing in this repository shows the registration; the package comment and ADR-0017 say where it is | `bindings@v0.20000.0/cgo.go:18-22`; owner, 2026-10-27 (the whole candidate)          |
| The binding and qdb-duck's submodule move to 2.0.0 together                                                             | the header and the library come from one release, and qdb-duck's master is at v2.0.0                                                                     | staying on 1.5.4 and pinning qdb-duck's package: leaves the base on a package qdb-duck no longer builds                                  | the `#sql` path on current qdb-duck            | the pins of the held branch become dead and its rebase drops them                                   | `docs/adr/0017-embedded-duckdb.md:43-46`; qdb-duck `21faaffe1`                       |
| The stubs are repointed to `go.duckdb.org/libduckdb/<os>-<arch>` and kept                                               | the binding moved the prebuilt modules; without the replacement a re-vendor commits half a gigabyte                                                      | dropping the stubs and vendoring the archives; pruning after each vendor through a make target                                           | a re-vendor stays a plain `go mod vendor`      | five stub module lines that must follow any later path move                                         | `third_party/AGENTS.md:19-24` (owner, 2026-10-14); `bindings@v0.20000.0/go.mod:6-10` |
| The bump, the repoint and the re-vendor are one commit                                                                  | a vendor/ out of step with go.mod and a replace directory with another module line both fail every `go` command, so no split keeps every commit building | three commits as the seed listed                                                                                                         | every commit builds and lints                  | one large diff, mostly generated binding code                                                       | Go's module and vendor rules; the workflow's commit rule (`/rr-workflow`)            |
| The describe map changes only where `TestDescribe` fails                                                                | the test compares the map with the live Arrow reader on both clusters, which is the only oracle this repository has for DuckDB's conversion              | rewriting the map from 2.0.0's `arrow_converter.cpp` by reading: a second hand copy with the same risk                                   | no untested case                               | a conversion change on an expression the test does not cover goes unseen until a client meets it    | `internal/olap/AGENTS.md:31-36`                                                      |
| ADR-0017 is edited in place                                                                                             | the decision (pairs, the bundle, the tags) is unchanged; the release number and the registration mechanism are details within its subject                | a superseding ADR                                                                                                                        | one ADR for the embedding                      | none                                                                                                | `docs/AGENTS.md`, ADRs (owner, 2026-10-14 and 2026-10-19)                            |
| The local 2.0.0 package for the build stage is qdb-duck's current master macOS arm64 package from Buildkite's artifacts | the local run then links the package Buildkite links; the static registration is provable only on a Linux or FreeBSD build either way                    | building qdb-duck locally (a full DuckDB build, and a different compiler); tests only in Buildkite (a CI build per describe map attempt) | `TestDescribe` runs before the push            | one artifact download, by whichever of Buildkite's artifact API or the R2 bucket holds it           | owner, 2026-10-27                                                                    |
| Symbol reads answering `(unknown)` on current qdbd are not investigated here                                            | the defect is qdb-duck's, the owner owns qdb-duck, and the log's Handoff already assigns it                                                              | a bound or a workaround in this repository                                                                                               | the unit stays a bump                          | the base may stay red on a qdb-duck defect after this unit                                          | `docs/log.md`, Handoff; memory `qdbd-defects-belong-to-rnd`; Open question 2         |

## Knowledge

Per commit, where each why lands.

- Commit 2 (`build(go)`): `third_party/AGENTS.md` carries the new
  module paths and the rule that a path move is the one stub change
  (Rationale: stubs repointed, one commit). Each stub's `lib.go`
  comment names its module. `go.mod` carries the versions; the vendor
  diff is generated and carries nothing.
- Commit 3 (`docs(olap)`): the olap package comment carries the
  registration claim and where it happens (Rationale: no registration
  call). `arrowTypeOf`'s overview names 2.0.0 and the client properties
  as re-read from 2.0.0's source.
- Commit 4 (`fix(olap)`, present only if `TestDescribe` fails): each
  changed case carries the 2.0.0 conversion rule it follows, read from
  `arrow_converter.cpp` at tag v2.0.0, with the test as its proof
  (Rationale: the test is the oracle).
- Commit 5 (`docs(build)`): the `.envrc` block carries the bundle's
  contents under 2.0.0, the header name, and the link consequence of
  the binding's init (Rationale: no registration call, as seen from the
  linker).
- Commit 6 (`docs(adr)`): ADR-0017 Decision 2 carries the registration
  mechanism, Decision 3 the release, the alternatives table the
  rejected registration call (Rationale rows 1, 2, 6).
- Commit 8 (`docs(log)`): Current state carries the held branch and
  what its merge waits on; the dated entry names where the facts went.
  The local-package decision and the `(unknown)` boundary are owner
  decisions of this conversation: the first lives in no document,
  because it is how one build stage ran and not a rule, and the second
  is already the log's Handoff item.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from the Design and Knowledge sections in the same commit as the
code and reconciles the code against this plan before the build-stage
message. Specific to this unit:

- Before commit 2, the build stage replaces the local `duck/` with
  qdb-duck's current master macOS arm64 package (owner, 2026-10-27),
  and runs `go test ./internal/olap/...` with `TestDescribe` first;
  the result decides whether commit 4 exists.
- Commit 3's conversion comment and commit 4's case comments depend on
  `src/common/arrow/arrow_converter.cpp` at qdb-duck's DuckDB submodule
  (tag v2.0.0): the executor reads the default client properties and
  the decimal rule there and writes only what it finds. A property the
  source no longer has is dropped from the comment, not paraphrased.
- If `TestDescribe` fails on a type the map cannot express with the
  Arrow types the encoders write (`internal/encoding/AGENTS.md`), the
  executor asks the owner before adding a case, because an encoder
  change is outside this unit.
- If the Buildkite build fails on Linux or FreeBSD at the link with
  `duckdb_register_static_extensions` undefined, the bundle lacks the
  loader on that platform: the executor reports it to the owner as a
  qdb-duck packaging defect and does not patch the link line.

## Commits

1. `docs(plan): duckdb-2-plan.md, the binding and qdb-duck's package move to DuckDB 2.0.0 together and the static bundle's extensions register through the binding` (this document)
2. `build(go): duckdb-go v2.20000.0, the stubs stand in for go.duckdb.org/libduckdb and vendor/ follows`
3. `docs(olap): the package comment says the binding registers the bundle's extensions before the first open, and the conversion comment names 2.0.0`
4. `fix(olap): the describe map follows 2.0.0's Arrow conversion where TestDescribe shows a change` (only if the test fails)
5. `docs(build): the .envrc duck/ comment names the registration loader inside the bundle and the header that declares it`
6. `docs(adr): ADR-0017 names 2.0.0 and the binding's registration of the bundle's linked extensions`
7. `/doc-discipline all docs/duckdb-2-plan.md internal/olap third_party .envrc docs/adr/0017-embedded-duckdb.md`, then `/doc-discipline all check` against the plan; findings are further small commits.
8. `docs(log): duckdb-2-plan.md deleted, facts moved to ADR-0017, third_party/AGENTS.md and the comments of internal/olap and .envrc`
9. Verify: push `sc-19567/rr-duckdb-2`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. The held branch `sc-19567/rr-readiness-drain` carries two artifact
   pins and two log commits about them that this unit makes dead, and
   it cannot fast-forward once the base moves. Recommendation: after
   this unit merges, rebase it onto the base, drop `1c7bc68`,
   `ab40f75`, `a00796f` and `49c04fb`, rewrite `589cfcb` to the In
   flight text of this plan, and build its head.
2. Symbol reads that answer `(unknown)` on current qdbd have not been
   observed under the 2.0.0 package. Recommendation: if this unit's
   build shows them, the unit still merges on a green build of every
   other step only if the owner says so; otherwise the base waits on
   the qdb-duck fix, and nothing in this repository works around it.
3. Which of Buildkite's artifact API and the R2 bucket holds qdb-duck's
   macOS package is unknown. Recommendation: the build stage tries the
   artifact API first and falls back to R2 (memory
   `ci-artifacts-r2-download`).
