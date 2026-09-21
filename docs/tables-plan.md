# v2 Tables: create and delete -- Plan

Status: draft. Working plan for the first M2 slice: `POST /api/v2/tables`
and `DELETE /api/v2/tables/{name}`, under a property test. Deleted when
the work lands; the wire contract moves to `docs/brief.md`
("/api/v2 endpoint sketch"), the handler rules to
`internal/httpapi/AGENTS.md`. No ADR: the shapes follow ADR-0010 and
plain REST, and no alternative was hard to reject.

## The wire contract

Both routes sit behind `requireBearer` and `withCompression`, like the
query; every error is an RFC 9457 problem (ADR-0010 6).

### POST /api/v2/tables

`Content-Type: application/json`, the body capped at 1 MiB through
`readBody`:

```json
{
  "name": "trades",
  "shard_size": 86400000,
  "columns": [
    { "name": "price", "type": "double" },
    { "name": "venue", "type": "symbol", "symtable": "venues" }
  ]
}
```

- `shard_size` is an integer number of milliseconds, required: the unit
  the C API and `qdb-api-go` take. No duration strings, so the REST API
  introduces no dialect next to qdbsh's.
- `type` is one of `blob`, `double`, `int64`, `string`, `symbol`,
  `timestamp` (`docs/brief.md`, "/api/v2 endpoint sketch"). `symtable`
  is required on a `symbol` column, as the C API requires it, and
  refused on any other.
- `$timestamp` is implied and never listed.

| Condition                                                                          | Status                                        |
| ---------------------------------------------------------------------------------- | --------------------------------------------- |
| created                                                                            | 201, `Location: /api/v2/tables/<name>`, empty |
| `Content-Type` not `application/json`                                              | 415                                           |
| body over the cap                                                                  | 413                                           |
| undecodable body; `shard_size` absent; an unknown `type`; the symtable rule broken | 400                                           |
| the table exists (`ErrAliasAlreadyExists`)                                         | 409                                           |
| anything else the cluster answered (a bad name, a zero shard size)                 | 400, `detail` the binding's message           |
| unreachable, breaker open, caller gone, no or bad bearer                           | ADR-0010's rows, unchanged                    |

### DELETE /api/v2/tables/{name}

No body. Removes the table and nothing else: a symtable is its own
entry, which other tables may share, and stays.

| Condition                                    | Status     |
| -------------------------------------------- | ---------- |
| removed                                      | 204, empty |
| no such table (`ErrAliasNotFound`)           | 404        |
| anything else the cluster answered           | 400        |
| unreachable, breaker open, caller gone, auth | ADR-0010's |

## Approach

- `internal/qdb` owns the vocabulary and the calls: a `Column`
  (`Name`, `Type`, `Symtable`) with the six type words mapped onto the
  binding's `TsColumnInfo`; `Cluster.CreateTable` and
  `Cluster.RemoveTable`, each one `Call` as the caller over the
  existing `Session.CreateTable` / `Session.RemoveTable`
  (`internal/qdb/cluster.go:237-246`), without `WithReadRetry` (neither
  is an idempotent read); `IsTableExists` and `IsTableNotFound`,
  predicates in the style of `IsClusterUnavailable`
  (`internal/qdb/breaker.go:108`), so `internal/httpapi` names no
  binding error.
- `internal/httpapi/tables.go`: the request type, its decode and shape
  check, `handleCreateTable`, `handleDeleteTable`,
  `registerTableRoutes`. The handlers test the two predicates first and
  hand everything else to `writeClusterError` with 400; the query's
  mapping is untouched (an unknown table in a query stays 400).
- Validation has one home (`internal/AGENTS.md`, Code): the handler
  checks shape only -- JSON, a present `shard_size`, a known type word,
  the symtable rule. Names, sizes and column counts are the C API's to
  judge.
- The `Location` path-escapes the name.

## Tests

- One property (`internal/httpapi/tables_test.go`): a generated schema
  (one to five columns over the six types, symtables named after the
  table) is created over HTTP; the table queried through
  `POST /api/v2/query` as JSON answers exactly its columns, in order,
  with the wire type names and empty `data`; a second create answers
  409; the delete answers 204, a second delete 404. The table and its
  symtables are removed on the iteration's cleanup, ErrAliasNotFound
  tolerated (the pattern of `internal/qdbtest/table/table.go:220-254`).
- The error rows, one by one: 415, 400 (undecodable, no `shard_size`,
  unknown type, symbol without symtable, symtable on a non-symbol),
  401 on both routes.
- Verified while writing the property, recorded here dated: a table
  re-created over a symtable that already exists is accepted (the
  flow's idempotency rests on it).

## The log

The log's Current state still describes M1 as waiting for a Buildkite
run of the base and M2 as gated on it; the owner's decision
(2026-09-21) is that table creation and ingest come first and the
first CI evidence is the flow. The log commit rewrites Current state
to that and leaves nothing of the earlier approach: M1 `done`, its
criteria paragraph gone; M2 `in progress`, no entry gate; Next 1 (push
and trigger) removed; Next names the remaining M2 slices; one dated
entry.

## Commits

1. `feat(qdb): a column is a name, one of six type words and a symtable`
2. `feat(qdb): Cluster.CreateTable and RemoveTable run as the caller`
3. `feat(qdb): IsTableExists and IsTableNotFound classify the binding's answers`
4. `feat(httpapi): the create-table request decodes and checks its shape`
5. `feat(httpapi): POST /api/v2/tables answers 201 with a Location`
6. `feat(httpapi): DELETE /api/v2/tables/{name} answers 204`
7. `test(httpapi): a generated schema is created, queried empty and deleted over HTTP`
8. `test(httpapi): the table routes' error rows`
9. `docs: the table endpoints are the brief's; no ADR-0015`
10. `docs(httpapi): AGENTS.md names the table handler rules`
11. `docs(log): M1 done; M2 in progress; the ingest slice is next`
12. `docs(plan): tables-plan.md deleted; facts moved to the brief and httpapi/AGENTS.md`

Commit 9 adds the DELETE row and the two contracts to the brief's
sketch and repoints every ADR-0015 mention (`docs/brief.md`,
`docs/log.md`, `docs/e2e.md`, `docs/e2e-v2-flow-plan.md`, ADR-0014
decision 6, one mechanical line). Each commit builds and passes lint;
the tests run `go test -p 1`.

## Remaining M2 slices, and what the owner fixed for them (2026-09-21)

1. Ingest core with CSV: `POST /api/v2/tables/{name}/rows`. The body
   cap is 64 MiB (the 1 MiB cap stays for queries, logins and the
   create). `?push-mode=transactional|fast|async`, default `fast`.
   `?deduplication-mode=drop|upsert`, absent meaning none;
   `?deduplication-columns=a,b` is required with either mode, since the
   columns say what a duplicate is and the mode only what happens to
   one.
2. The NDJSON and Arrow IPC parsers; `Content-Encoding: gzip|zstd`.
3. `tests/e2e/tools/e2etool` (`gen`, `tocsv`).
4. `flow.sh` and `make test-flow`; the flow drops its tables through
   the DELETE of this slice.
5. `scripts/cicd/40.test-e2e.sh`; `e2e-v2-flow-plan.md` deleted.

## Decision log (2026-09-21)

| Decision                                       | Why                                                                      | Rejected                                                  |
| ---------------------------------------------- | ------------------------------------------------------------------------ | --------------------------------------------------------- |
| No ADR for the two endpoints                   | ADR-0010's conventions and plain REST; nothing here was hard to decide   | ADR-0015                                                  |
| `shard_size` is integer milliseconds, required | the C API's unit; any string syntax short of qdbsh's is a second dialect | Go duration strings; QuasarDB duration strings; a default |
| 201 with `Location`, empty body                | REST's answer to a created resource                                      | 200 with a body                                           |
| 409 for an existing table, 404 for a missing   | the statuses a REST client acts on; two rows beside ADR-0010's table     | the plain 400 of "the cluster answered"                   |
| `symtable` required on a symbol column         | the C API requires it                                                    | a derived default name                                    |
| DELETE removes the table only                  | a symtable is a shared entry                                             | removing the symtables a table names                      |
| Plural path, `/api/v2/tables/{name}`           | one resource, one spelling, as the brief's sketch has it                 | `/api/v2/table/{name}`                                    |
