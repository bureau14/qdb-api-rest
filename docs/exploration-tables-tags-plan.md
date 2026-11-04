# Exploration: the table list, the table and its tags

Thread: M8 exploration routes
Status: approved

## Outcome

Five routes of M8 serve behind the bearer edge with ADR-0010's problems:

- `GET /api/v2/tables` answers `{"tables":[...]}`, the cluster's table
  names in its order, `$qdb.` tables included, under an optional
  `?prefix=` applied in Go.
- `GET /api/v2/tables/{name}` answers the table in the create body's
  vocabulary, `$timestamp` listed first, with its shard size, TTL and
  tags.
- `PUT /api/v2/tables/{name}/tags/{tag}` attaches the tag, and `DELETE`
  on the same path detaches it. Both answer 204 whether or not anything
  changed.
- `GET /api/v2/tags/{tag}` answers `{"entries":[...]}`, the names of the
  entries that carry the tag, and 404 when no entry carries it.

`internal/qdb` gains `Cluster.Table`, `Cluster.AttachTag`,
`Cluster.DetachTag`, `Cluster.Find` and `Cluster.Tagged`, each timed
under its own `Operation`. `TestRoundtrip` and the shell round trip list
their tables under a prefix, read a table against its create body,
attach a tag, read it on the table and its entries, and detach it. The
test fixtures and the shell driver remove the tag entries they leave.

Left for later:

- `GET /api/v2/tags`, the list of every tag name, is not built, and no
  story is filed (owner, 2026-11-04). qdbd 3.15 has no tag enumeration
  (Verified facts 1). The route leaves M8 for good, because it is not
  valuable enough to work around qdbd for (owner, 2026-11-04). The brief's
  sketch row goes, and the M8 criteria count seven routes.
- The cluster and node statistics routes, `GET /api/v2/cluster` and
  `GET /api/v2/cluster/nodes/{id}`, are the next unit. They need a
  qdb-api-go fix first, because `HandleType.Statistics` never closes the
  direct handles it opens
  (`vendor/github.com/bureau14/qdb-api-go/v3/statistics.go:118-141`,
  `direct.go:36-39`).

## Verified facts

All qdbsh facts were checked against the insecure test cluster (port
2836, qdbd 3.15.0.dev0) on 2026-11-04, on `qdbtest_` entries that were
removed afterwards.

1. A tag is an entry of its own, of type `tag` (`get_type` answers
   `tag`, and `prefix_get` lists it as `- tag`). `$qdb.tagroot` does not
   exist. `find(tag='$qdb.tagroot')` answers "An entry matching the
   provided alias cannot be found." even with a tag attached, and
   `get_tagged $qdb.tagroot` answers nothing. `show tags` is an invalid
   query. `prefix_count $qdb` is 100003 on the test cluster.
2. A tag entry outlives its carriers. Detaching the last carrier and
   dropping the table both leave the `tag` entry, and `remove <tag>`
   removes it.
3. Dropping a table removes it from its tags. Afterwards
   `find(tag='<tag>')` answers alias-not-found.
4. `find(tag='<tag>')` answers alias-not-found for a tag that never
   existed and for a tag entry no entry carries.
   `get_tagged <tag>` answers an empty list in both cases.
5. Attaching a tag the entry already has answers "The entry is already
   marked with the provided tag." Detaching a tag it lacks answers "The
   entry is not marked with the provided tag." The binding's sentinels
   are `ErrTagAlreadySet` and `ErrTagNotSet`
   (`vendor/github.com/bureau14/qdb-api-go/v3/error.go:94-95`), matched
   through `errors.Is` (`error.go:180`, `:256`).
6. `attach_tag`, `detach_tag` and `get_tags` on a missing entry answer
   alias-not-found, which `qdb.IsTableNotFound` classifies
   (`internal/qdb/table.go:288`).
7. No quoting of a `'` inside a find tag was found. `\'`, `''` and a
   double-quoted string are all invalid queries. The binding's own
   `Find.buildQuery` writes the tag between single quotes unescaped
   (`vendor/.../find.go:82-100`).
8. The binding: `Entry.AttachTag`, `DetachTag` and `GetTags`
   (`vendor/.../entry.go:156-285`), reached through
   `HandleType.Table(alias)`, and `HandleType.Find().ExecuteString(q)`
   (`find.go:59-80`), which wraps its error with the query. `Entry.Remove`
   removes an entry of any type (`entry.go:33`). The fixture already
   removes symtables, which are blobs, through `Session.RemoveTable`
   (`internal/qdbtest/table/table.go:468-503`).
9. The Arrow types: `TsColumnType.ArrowType` maps int64, double,
   timestamp (ns), blob (binary), and string and symbol (utf8)
   (`vendor/.../entry_timeseries_common.go:175-192`).
   `qdbapi.ColumnTypeOfArrow` is its inverse for the accepted set
   (`:194`). A symbol field is told from a string field only by
   `qdb.MetaSymtable` (`internal/qdb/table.go:198-206`).
10. `schemaOfMetadata` answers `$timestamp` first, the columns in order,
    `qdb:shard_size` and `qdb:ttl` as integer milliseconds strings
    (`internal/qdb/table.go:180-218`), and `Session.tableSchema` fetches
    one table's metadata on the held session (`table.go:222-231`).
11. No route answers a table or a list today
    (`internal/httpapi/httpapi.go:104-118`, `tables.go:129-132`), and
    every JSON body is encoded inline (`login.go:53`, `login.go:234`,
    `rows.go:144`).
12. The fixture table names are `qdbtest_` and 16 drawn lowercase
    letters (`internal/qdbtest/table/table.go:224-252`), so a table's
    own name as a prefix matches that table alone.
13. The shell driver knows each server's URLs but neither cluster's URI,
    and nothing calls the `qdbsh` wrapper (`tests/e2e/common.sh:51-58`).
    The Makefile has both URIs and the secure key files
    (`tests/e2e/Makefile:25`, `:77-83`). qdbsh takes `--cluster`,
    `--cluster-public-key-file` and `--user-security-file`
    (`qdbsh --help`).
14. `docs/e2e.md:218-220`, The driver, omits `--jdbc-jar`, which
    `tests/e2e/round-trip.sh:10`, `:38` and `:52` require.

## Design

### internal/qdb/cluster.go

The `Operation` block gains four values in the order of the methods
that use them.

```go
const (
	// ... the eight existing values ...
	OpTable     Operation = "table"
	OpAttachTag Operation = "attach_tag"
	OpDetachTag Operation = "detach_tag"
	OpFind      Operation = "find"
)
```

### internal/qdb/table.go

After `TableSchemas`, before `IsTableExists`.

```go
// TableInfo is one table as GET /api/v2/tables/{name} answers it: its
// schema, as TableSchemas builds it, and its tags. [Rationale 6]
type TableInfo struct {
	TableSchema
	Tags []string
}
```

```go
// tags answers the tags of the named entry on the held session, in the
// cluster's order. An unknown entry passes through unchanged, so
// IsTableNotFound classifies it. [Verified 6]
func (s *Session) tags(name string) ([]string, error) {
	// style: doc comment alone
	// because: one binding call
}
```

```go
// Table answers the named table as the user: its schema and its tags,
// through one leased session. An unknown table is IsTableNotFound. It
// is retried once on a retryable failure, as a read is. [Verified 10]
func (c *Cluster) Table(ctx context.Context, u User, name string) (TableInfo, error) {
	// style: doc comment + inline notes
	// because: one line needs its why, the single lease
	//
	// at the ObservedCall: the schema and the tags share one session,
	// because one checkout per request is what the pool and the budget
	// are built around [TableSchemas, table.go:238-242]
}
```

```go
// typeWords is the schema vocabulary of each column type, the inverse of
// columnTypes. [table.go:31-38]
var typeWords = ...
```

```go
// ColumnOfField answers a field of a TableSchema as a Column in the
// schema vocabulary. A field carrying MetaSymtable is a symbol column of
// that symtable. [Verified 9]
func ColumnOfField(f arrow.Field) (Column, error) {
	// style: doc comment + inline notes
	// because: two lines need their why
	//
	// at the MetaSymtable lookup: the symtable is the only mark of a
	// symbol column, because a symbol reads as a string in Arrow
	// [Verified 9]
	// at ColumnTypeOfArrow's failure: an error naming the field, because
	// TableSchemas builds every field from the binding's own types and a
	// failure means the binding changed [schemaOfMetadata, table.go:208]
}
```

```go
// ErrInvalidTag is the error for a tag a find expression cannot carry,
// one holding a single quote. It is the caller's error and is found
// before any session is leased. [Verified 7, Rationale 8]
var ErrInvalidTag = errors.New("qdb: invalid tag")
```

### internal/qdb/tag.go (new)

```go
// AttachTag attaches the tag to the named entry as the user. A tag the
// entry already carries is IsTagAlreadySet, and an unknown entry is
// IsTableNotFound. It is never retried. [Verified 5, 6]
func (c *Cluster) AttachTag(ctx context.Context, u User, name, tag string) error {
	// style: doc comment + inline notes
	// because: one line needs its why
	//
	// at the missing WithReadRetry: only reads are safe to run twice
	// [CreateTable, table.go:72-74]
}
```

```go
// DetachTag detaches the tag from the named entry as the user. A tag the
// entry does not carry is IsTagNotSet, and an unknown entry is
// IsTableNotFound. The tag's own entry stays. It is never retried.
// [Verified 2, 5, 6]
func (c *Cluster) DetachTag(ctx context.Context, u User, name, tag string) error {
	// style: doc comment alone
	// because: the retry's reason sits at AttachTag, and the rest is one
	// call
}
```

```go
// IsTagAlreadySet reports whether err means the entry already carries
// the tag. The HTTP layer reads it as success. [Verified 5, Rationale 4]
func IsTagAlreadySet(err error) bool

// IsTagNotSet reports whether err means the entry does not carry the
// tag. The HTTP layer reads it as success. [Verified 5, Rationale 4]
func IsTagNotSet(err error) bool
```

```go
// Find answers the entry names a find expression matches, as the user,
// in the cluster's order. An expression that matches nothing is
// IsTableNotFound. It is retried once on a retryable failure, as a read
// is. [Verified 4, 8]
func (c *Cluster) Find(ctx context.Context, u User, expr string) ([]string, error) {
	// style: doc comment alone
	// because: one binding call under the read retry, and the contract
	// says the rest
}
```

```go
// Tagged answers the names of the entries that carry the tag, as the
// user, through Find. A tag no entry carries is IsTableNotFound, and a
// tag holding a single quote is ErrInvalidTag. [Verified 4, 7]
func (c *Cluster) Tagged(ctx context.Context, u User, tag string) ([]string, error) {
	// style: doc comment + inline notes
	// because: two lines need their why
	//
	// at the quote check: the find grammar has no escape for a single
	// quote, so such a tag cannot be named in an expression [Verified 7]
	// at the expression: Find is the core, because the v1 find wart wraps
	// the same call (owner, 2026-10-26) [docs/brief.md, endpoint sketch]
}
```

### internal/httpapi/tables.go

`columnRequest` gains `omitempty` on `Symtable`, so the table body
leaves it off a non-symbol column. Its doc comment becomes: one column
of the create body and of the table body, in the schema vocabulary,
whose symtable belongs to a symbol column only.

```go
// tablesResponse is the list body. The names sit under a key, so a field
// can join them later without a break. [Rationale 3]
type tablesResponse struct {
	Tables []string `json:"tables"`
}
```

```go
// tableResponse is the table body: the create body's fields with
// $timestamp as the first column, plus the TTL and the tags. Both
// durations are integer milliseconds. [Rationale 2]
type tableResponse struct {
	Name      string          `json:"name"`
	ShardSize int64           `json:"shard_size"`
	TTL       int64           `json:"ttl"`
	Columns   []columnRequest `json:"columns"`
	Tags      []string        `json:"tags"`
}
```

```go
// withPrefix answers the names that start with the prefix, in their
// order. The empty prefix keeps every name. [docs/brief.md, sketch]
func withPrefix(names []string, prefix string) []string {
	// style: doc comment alone
	// because: a filter
}
```

```go
// handleListTables answers the table names as the caller sees them,
// under ?prefix= when given. Any cluster error is the caller's 400.
func handleListTables(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + inline notes
	// because: one line needs its why
	//
	// at withPrefix: the prefix is applied in Go, because show tables
	// takes none (owner, 2026-10-26) [docs/brief.md, sketch;
	// table.go:94-101]
	// at the encode: tags and names are never null on the wire, so an
	// empty list encodes as [] [Rationale 3]
}
```

```go
// tableResponseOf renders the table in the create vocabulary. It fails
// only on a field ColumnOfField cannot name. [Verified 9, 10]
func tableResponseOf(t qdb.TableInfo) (tableResponse, error) {
	// style: doc comment + inline notes
	// because: two lines need their why
	//
	// at the metadata parse: the shard size and TTL travel as integer
	// millisecond strings on the schema [Verified 10]
	// at the columns: $timestamp is listed first, as every query
	// returns it (owner, 2026-11-04) [Rationale 2]
}
```

```go
// handleGetTable answers the table the path names, its columns and its
// tags. A name the cluster does not know is 404, and anything else the
// cluster answered is the caller's 400.
func handleGetTable(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + inline notes
	// because: one line needs its why
	//
	// at the render failure: 500, because a field the binding cannot
	// name is this process's fault [internal/httpapi/AGENTS.md, 500 is
	// reserved for this process]
}
```

`registerTableRoutes` gains `GET tablesPath` and `GET tablesPath/{name}`
in the same `handleData(withCompression(requireBearer(...)))` form. Its
doc comment is unchanged.

### internal/httpapi/tags.go (new)

```go
// tagsPath is the tag collection. A tag is tagsPath + "/" + its name.
const tagsPath = "/api/v2/tags"

// entriesResponse is the body of GET /api/v2/tags/{tag}. [Rationale 3]
type entriesResponse struct {
	Entries []string `json:"entries"`
}
```

```go
// handleAttachTag attaches the path's tag to the path's table and
// answers 204, also when the table already carries it. A table the
// cluster does not know is 404.
func handleAttachTag(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + inline notes
	// because: one line needs its why
	//
	// at IsTagAlreadySet: PUT is idempotent, so a retry after a lost
	// answer succeeds (owner, 2026-11-04) [Rationale 4]
}
```

```go
// handleDetachTag detaches the path's tag from the path's table and
// answers 204, also when the table does not carry it. A table the
// cluster does not know is 404.
func handleDetachTag(w http.ResponseWriter, r *http.Request) {
	// style: doc comment alone
	// because: the idempotence reason sits at handleAttachTag
}
```

```go
// handleTagged answers the names of the entries that carry the path's
// tag. A tag no entry carries is 404, a tag with a single quote is 400,
// and anything else the cluster answered is the caller's 400.
func handleTagged(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + inline notes
	// because: one line needs its why
	//
	// at the 404: the tag is the route's resource, as a table is on the
	// read and the delete (owner, 2026-11-04) [Rationale 5]
}
```

```go
// registerTagRoutes serves the tag routes behind the bearer middleware.
// The mux patterns fix method and path.
func registerTagRoutes(mux *http.ServeMux)
```

`NewHandler` calls `registerTagRoutes` after `registerTableRoutes`.

### internal/qdbtest/table/table.go

```go
// RemoveTagOnCleanupAs removes the tag's entry on the test's cleanup as
// the user, tolerating a tag that is already gone. A failed removal is
// reported and never fatal, so a leak is visible. [Verified 2, 8]
func RemoveTagOnCleanupAs(t T, c *qdb.Cluster, u qdb.User, tag string) {
	// style: doc comment + inline notes
	// because: one line needs its why
	//
	// at the removal: a tag entry outlives its carriers, so neither the
	// detach nor the table's removal removes it [Verified 2]
	// at RemoveTable: the call removes an entry of any type, as it
	// removes a symtable [Verified 8]
}
```

### internal/httpapi/tables_test.go and tags_test.go (helpers)

```go
// listTables gets the table list under the prefix and decodes it.
func (s server) listTables(t table.T, prefix string) tablesResponse

// getTable gets the named table and decodes it.
func (s server) getTable(t table.T, name string) tableResponse

// tableResponseOfCreate is the table body the create body promises, with
// $timestamp first, a TTL of 0 and the given tags.
func tableResponseOfCreate(req createTableRequest, tags []string) tableResponse

// attachTag, detachTag and tagged send the three tag requests.
```

### internal/httpapi/errors_test.go

New rows, each under its route's comment:

```
// the table list and the table
"list: no token"          GET  /api/v2/tables                    401 Bearer
"table: no token"         GET  /api/v2/tables/qdbtest_err        401 Bearer
"table: no such table"    GET  /api/v2/tables/qdbtest_err        404
// the tags
"attach: no token"        PUT  .../qdbtest_err/tags/qdbtest_tag  401 Bearer
"attach: no such table"   PUT  .../qdbtest_err/tags/qdbtest_tag  404
"detach: no such table"   DELETE same path                       404
"tagged: no token"        GET  /api/v2/tags/qdbtest_tag          401 Bearer
"tagged: no such tag"     GET  /api/v2/tags/qdbtest_tag_none     404
"tagged: quote in tag"    GET  /api/v2/tags/qdbtest_a'b          400
```

The secure anonymous block gains one row per new data route, 401
`invalid_token`.

### internal/httpapi/roundtrip_test.go, TestRoundtrip

The step list grows, and the step comments are rewritten whole:

```go
// 3. create each table (201 with its Location). Then each table's own
//    name as a prefix lists that table alone, and each table reads back
//    as its create body promises, $timestamp first and no tags
//    [Verified 12]
// ...
// 7. tag each table with one drawn tag, removed on cleanup, attached
//    twice (204 both times). The tag's entries are the tables, the table
//    body lists the tag, and a detach twice (204 both times) leaves the
//    tag on no entry, which is 404 [Rationale 4, 5; Verified 2, 4]
// 8. delete each table, as step 7 did before
```

The tag is `qdbtest_` and 16 drawn letters, so concurrent iterations
never share one. `RemoveTagOnCleanupAs` is registered before the first
attach. The tag's entries are compared as a set, because the find
answers in the cluster's order.

### tests/e2e/round-trip.sh and common.sh

```bash
# The driver gains --insecure-cluster <uri>, --secure-cluster <uri>,
# --secure-cluster-key <file> and --secure-user-key <file>, which the
# Makefile passes from QDB_URI, QDB_URI_SECURE and the repo-root key
# files. [Verified 13]
#
# remove_tag <cluster args...> <tag>: removes the tag entry through the
#   qdbsh wrapper, tolerating alias-not-found, so the round trip starts
#   from a clean state as create_table does. [Verified 2]
#
# list_back <out> <url> <token> <table>: GET /api/v2/tables?prefix=<table>
#   answers {"tables":["<table>"]}, compared with jq.
#
# table_back <out> <url> <token> <table>: GET /api/v2/tables/<table>
#   answers the create body with $timestamp first, "ttl":0 and no tags,
#   compared by jq -S against create_body put through the same jq filter.
#
# tag_back <out> <url> <token> <tag> <tables...>: PUT each table's tag
#   (204), GET /api/v2/tags/<tag> lists the tables as a set, the table
#   body lists the tag, DELETE each (204), and GET /api/v2/tags/<tag> is
#   404. The tag is e2e_tag, fixed, so a run reuses the name the previous
#   one removed.
```

`run_cluster` calls `list_back` and `table_back` after each
`create_table`, and `tag_back` once after the format loop and before
the scrape.

### Documents

`internal/httpapi/AGENTS.md`, Handlers, gains:

```
- The table list is `GET /api/v2/tables`, over `Cluster.Tables`, with
  `?prefix=` applied in Go, answering `{"tables":[...]}`.
- The table is `GET /api/v2/tables/{name}`, over `Cluster.Table`,
  answering the create body's fields with `$timestamp` first, `ttl` and
  `tags`, durations in milliseconds. 404 on `qdb.IsTableNotFound`.
- The tag routes. PUT and DELETE on `/api/v2/tables/{name}/tags/{tag}`
  answer 204, and `IsTagAlreadySet` and `IsTagNotSet` are success,
  because both methods are idempotent. 404 on an unknown table.
  `GET /api/v2/tags/{tag}` answers `{"entries":[...]}` through
  `Cluster.Tagged`, 404 for a tag no entry carries, 400 for
  `qdb.ErrInvalidTag`. There is no route listing every tag, because
  qdbd has no tag enumeration.
- A list body is an object with a named array, never a bare array.
```

`internal/AGENTS.md`, Code, the table-list bullet gains `Cluster.Table`
(the schema and the tags on one leased session) and a new bullet:

```
- A tag is an entry of its own in qdbd, and it outlives its carriers: a
  detach or a table removal leaves it. `Cluster.AttachTag` and
  `DetachTag` are never retried. `Cluster.Find` is the find core, and
  `Cluster.Tagged` builds its expression, refusing a single quote
  (`ErrInvalidTag`), because the find grammar has no escape for one.
  A find that matches nothing answers alias-not-found.
```

`internal/AGENTS.md`, Tests, the table fixture bullet gains
`RemoveTagOnCleanupAs`.

`docs/e2e.md`, The v2 round trip, gains the list and table reads in
step 2 and a tag step after step 7. The driver section gains the four
cluster options and `--jdbc-jar`. `tests/e2e/AGENTS.md` gains: the
round trip's tag is fixed and removed through qdbsh first.

`docs/brief.md`, the sketch row of `GET /api/v2/tables/{name}` becomes
the create vocabulary with `$timestamp` first. The `GET /api/v2/tags` row
is removed, and the paragraph below the sketch says in one sentence that
qdbd has no tag enumeration, so no route lists every tag. The Milestones
line of M8 drops "list" from the tag routes.

`docs/log.md`, Current state: M7 is done, closed 2026-11-04 (owner), and
the stress budget step leaves the Next list. M8 is in progress. The M8
exit criteria count seven exploration routes, and "lists the tag and its
entries" becomes "lists the tag's entries". One dated entry records both
milestone events as owner decisions, pointing at `docs/brief.md`.

## Rationale

| decision                                                                                                                                  | why                                                                                                  | rejected, and why                                                                                                                                    | gained                                             | given up                                                  | settled by                                          |
| ----------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------- |
| 1. The unit is slices A and B, and `GET /api/v2/tags` leaves M8 for good with no story filed                                              | qdbd has no tag enumeration, and the route is not valuable enough to work around that                | a scan of every entry with a type lookup each, over 100000 round trips on the test cluster; a required `?prefix=` bound, which cannot list every tag | no slow route ships                                | the route, and M8's "lists the tag" as the brief wrote it | owner, 2026-11-04                                   |
| 2. The table body is the create body's shape with `$timestamp` as the first column, plus `ttl` and `tags`, in the schema vocabulary       | the body lists every column a query returns                                                          | `$timestamp` implied as on create; Arrow schema JSON, where a symbol reads as utf8                                                                   | one vocabulary over HTTP, symbol a type of its own | the body does not post back into create unchanged         | owner, 2026-11-04                                   |
| 3. List bodies are objects with a named array                                                                                             | a field can join later without a break                                                               | a bare array                                                                                                                                         | room for a count or a cursor                       | one key more to read                                      | owner, 2026-11-04                                   |
| 4. Attach and detach answer 204 when nothing changes                                                                                      | PUT and DELETE are idempotent, so a retry after a lost answer succeeds                               | 409 for an attached tag and 404 for a missing one                                                                                                    | retries need no special case                       | a client cannot see whether its call changed anything     | owner, 2026-11-04                                   |
| 5. A tag no entry carries is 404 on `GET /api/v2/tags/{tag}`                                                                              | the tag is the route's resource, as a table is on the read and the delete                            | 200 with an empty list                                                                                                                               | a typo is visible                                  | an orphaned tag entry and a missing tag read the same     | owner, 2026-11-04                                   |
| 6. `Cluster.Table` answers the schema and the tags on one leased session, as `TableInfo`                                                  | one checkout per request, as `TableSchemas` holds                                                    | `TableSchemas` filtered to one name, which fetches every table; two calls, two checkouts                                                             | one round trip pair per request                    | a type beside `TableSchema`                               | proposal                                            |
| 7. The HTTP renderer maps an Arrow field back to the vocabulary through `qdb.ColumnOfField`                                               | Arrow is the neutral form, and the Flight catalog reads the same schema                              | a second path from the C API metadata to JSON                                                                                                        | one builder of a table's shape                     | a field's type read back through `ColumnTypeOfArrow`      | proposal                                            |
| 8. `Cluster.Tagged` refuses a tag holding `'` with `ErrInvalidTag`, 400                                                                   | no quoting of `'` in a find expression exists                                                        | `GetTagged`, which takes no expression but answers an empty list for a missing tag and is not the find core                                          | no expression can be broken out of                 | such a tag cannot be listed, though qdbd allows it        | proposal                                            |
| 9. Tests remove the tag entries they make: the Go fixture on cleanup, the shell driver through qdbsh at the start with one fixed tag name | it matches how the fixtures treat tables, and the server keeps qdbd's tag semantics                  | detach removing an unused tag entry, which races with another client's attach; leaving them                                                          | clean clusters                                     | the driver takes each cluster's URI and key files         | owner, 2026-11-04                                   |
| 10. The tag routes accept any entry under `/tables/{name}`, with no type check                                                            | the binding tags any entry, and a check costs a round trip                                           | a metadata call before every attach                                                                                                                  | one call per request                               | a symtable can be tagged through a table route            | proposal                                            |
| 11. New `Operation` values `table`, `attach_tag`, `detach_tag` and `find`, with no Go test                                                | every leased call is timed under its operation, and the metrics have no Go tests (owner, 2026-11-02) | reusing `OpTableSchemas` for one table                                                                                                               | the histogram tells the routes apart               | none                                                      | `internal/AGENTS.md`, Logging and metrics and Tests |
| 12. M7 closes now and M8 opens with this unit                                                                                             | the owner closes M7 without waiting for the stress budget run                                        | landing this unit with M7 still in progress, or waiting for the run                                                                                  | M8's entry criterion holds                         | the budget step is no longer an M7 exit criterion         | owner, 2026-11-04                                   |
| 13. The e2e documents commit adds the missing `--jdbc-jar` to `docs/e2e.md`, The driver                                                   | pragmatic, since the commit rewrites the driver's usage anyway                                       | a commit of its own                                                                                                                                  | one usage block matching the script                | none                                                      | owner, 2026-11-04                                   |

## Knowledge

- Commit 2: `TableInfo`, `Session.tags`, `Cluster.Table` comments
  (Verified 6, 10; Rationale 6 inline at the `ObservedCall`). The
  `OpTable` value. `internal/AGENTS.md` follows in commit 19.
- Commit 3: `typeWords`, `ColumnOfField` (Verified 9; Rationale 7 is
  `plan only`, since no reader of the function proposes a second path).
- Commits 4 to 5: the handler comments (Rationale 2, 3 inline;
  the brief's prefix decision inline at `withPrefix`).
- Commits 8 to 11: `tag.go` comments (Verified 2, 4, 5, 6, 7; Rationale 8
  inline at the quote check). Rationale 10 is `plan only`.
- Commit 12: the fixture comment (Verified 2, 8).
- Commits 13 to 14: the tag handler comments (Rationale 4, 5 inline).
- Commit 19: `internal/httpapi/AGENTS.md` (Rationale 2, 3, 4, 5,
  and Verified 1 as "qdbd has no tag enumeration"), `internal/AGENTS.md`
  (Verified 2, 7; Rationale 8, 9).
- Commit 20: `docs/e2e.md` and `tests/e2e/AGENTS.md` (Rationale 9;
  Verified 13, 14).
- Commit 21: `docs/brief.md` (Rationale 1, 2).
- Commit 22: `docs/log.md`, Current state and one entry (Rationale 1, 12).
- Rationale 1's rejected alternatives land in `internal/httpapi/AGENTS.md`
  as the one sentence above. Rationale 11 cites existing rules.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code and reconciles
the code against this plan before the build-stage message.

- Before commit 3, check that `qdbapi.ColumnTypeOfArrow` answers
  `TsColumnString` for utf8, so that `ColumnOfField` turns it into a
  symbol only on `MetaSymtable`.
- Before commit 10, check that the binding's `ExecuteString` error on a
  find that matches nothing satisfies `errors.Is(err,
qdbapi.ErrAliasNotFound)`. If it does not, `Tagged`'s contract and
  the 404 mapping are a question to the owner.
- Before commit 17, check that the secure test user may remove a tag
  entry through qdbsh. If it may not, the removal is a question to the
  owner.

## Commits

1. `docs(plan): exploration-tables-tags-plan.md, the table list, the table and its tags`
2. `feat(qdb): Cluster.Table answers one table's schema and tags through one leased session`
3. `feat(qdb): ColumnOfField names a schema field in the create vocabulary`
4. `feat(httpapi): GET /api/v2/tables answers the table names under an optional prefix`
5. `feat(httpapi): GET /api/v2/tables/{name} answers the table in the create vocabulary with its tags`
6. `test(httpapi): the error rows cover the table list and the table`
7. `test(httpapi): the round trip lists each table under its name and reads it against its create body`
8. `feat(qdb): IsTagAlreadySet and IsTagNotSet classify the binding's tag answers`
9. `feat(qdb): Cluster.AttachTag and Cluster.DetachTag tag an entry through one leased session`
10. `feat(qdb): Cluster.Find answers the entry names of a find expression`
11. `feat(qdb): Cluster.Tagged answers the entries that carry a tag, refusing a single quote`
12. `test(qdbtest): RemoveTagOnCleanupAs removes a tag entry on the test's cleanup`
13. `feat(httpapi): PUT and DELETE /api/v2/tables/{name}/tags/{tag} answer 204, 404 on an unknown table`
14. `feat(httpapi): GET /api/v2/tags/{tag} answers the entries that carry the tag, 404 on none`
15. `test(httpapi): the error rows cover the tag routes`
16. `test(httpapi): the round trip attaches a tag, reads it on its entries and its tables, and detaches it`
17. `test(e2e): the driver takes each cluster's uri and key files for qdbsh`
18. `test(e2e): the round trip lists and reads each table and tags them under a fixed tag`
19. `docs(agents): the httpapi and internal agents files name the table, the tag routes and their cores`
20. `docs(e2e): the round trip's exploration steps and the driver's options`
21. `docs(brief): the table route answers the create vocabulary, and no route lists every tag`
22. `docs(log): M7 closed and M8 in progress over seven exploration routes`
23. `/doc-discipline all internal/qdb internal/httpapi internal/qdbtest tests/e2e`, with any fixes as `docs(...)` commits
24. `/doc-discipline check docs/exploration-tables-tags-plan.md`, with any fixes as `docs(...)` commits
25. `docs(log): exploration-tables-tags-plan.md deleted, facts moved to internal/AGENTS.md, internal/httpapi/AGENTS.md, docs/e2e.md, tests/e2e/AGENTS.md, docs/brief.md and the comments of internal/qdb, internal/httpapi and internal/qdbtest`
26. Verify: push `sc-19567/rr-exploration-tables-tags`, build its head in
    Buildkite, wait for the result. Green: report the build number. Red:
    fix with further small commits on this branch, push, build again.

## Open questions and recommendations

none
