# Cluster and node statistics routes -- Plan

Thread: M8 cluster and node statistics routes
Status: approved

## Outcome

When the unit lands, two exploration routes serve behind the bearer
edge, with ADR-0010's problems:

- `GET /api/v2/cluster` answers `{"nodes":[...]}`, one object per node of
  the cluster, sorted by address. Each object is the binding's
  `Statistics` struct as its JSON tags spell it, plus an `id` key holding
  the node's address, `<ip>:<port>`.
- `GET /api/v2/cluster/nodes/{id}` answers the same object for the node
  whose address is `{id}`. An address that is not one of the cluster's
  endpoints is 404, and the server never connects to it.

A node the server cannot connect to fails either route with 503 and no
`Retry-After`, and it does not feed the breaker. `internal/qdb` gains
the statistics core (`Cluster.Statistics`, `Cluster.NodeStatistics`)
over qdb-api-go's `HandleType.Statistics` and `HandleType.NodeStatistics`.
`TestErrorRows` covers both routes. `TestRoundtrip` gains a step that
checks the status and the node address and reads no counter. The e2e
driver gains no step, and `docs/e2e.md` records why.

The vendored qdb-api-go moves to PR #131's head, `88106da`, and the unit
re-points it at #131's squash commit on `master` before it merges.

Completing the unit closes M8 (owner, 2026-11-05), so commit 15 marks
M8 done in `docs/log.md`. The unit leaves nothing for later.

## Verified facts

- qdb-api-go `88106da` (branch `sc-20119/read-one-node-s-statistics-by-uri`,
  PR #131, stacked on #130) differs from the vendored `6afd5e8` in
  `statistics.go` and `statistics_test.go` only, and its `go.mod` and
  `go.sum` are unchanged (`git -C ~/git/qdb-api-go diff 6afd5e8 88106da --stat`,
  2026-11-05). `88106da` is a docs-only commit on top of `9afa69e`.
- At `88106da`, `HandleType.NodeStatistics(nodeURI string) (Statistics, error)`
  calls `DirectConnect(nodeURI)`, defers the direct handle's `Close`, and
  reads the node (`statistics.go:109-118` at `88106da`).
- At `88106da`, `HandleType.Statistics() (map[string]Statistics, error)`
  reads `h.Cluster().Endpoints()` and calls `NodeStatistics` per endpoint,
  keyed by `endpoint.URI()`. It returns on the first node that fails
  (`statistics.go:121-140` at `88106da`).
- `Endpoint.URI()` is `fmt.Sprintf("%s:%d", Address, Port)`, with no
  scheme (`vendor/github.com/bureau14/qdb-api-go/v3/cluster.go:73-76`).
- `DirectConnect` fails with a plain `fmt.Errorf("unable to connect to node with URI: %s", ...)`
  and not with an `ErrorType` (`vendor/github.com/bureau14/qdb-api-go/v3/direct.go:48-51`).
  So `qdbapi.IsClusterUnavailable` and `qdbapi.IsRetryable` are both
  false for it (`vendor/github.com/bureau14/qdb-api-go/v3/error.go:312-326`, `:396-410`),
  `feedBreaker` records it as a success (`internal/qdb/cluster.go:379-389`),
  and `writeClusterError` would answer it 400.
- `getStatistics` reads each leaf through `Blob(...).Get()` or
  `Integer(...).Get()` on the direct handle, so a failure after the
  connect is an `ErrorType` (`vendor/github.com/bureau14/qdb-api-go/v3/statistics.go:54-99`).
  Its one plain error, "could not retrieve all values", is reached only
  for a field kind the struct does not have.
- All 22 keys the `Statistics` struct reads exist under
  `$qdb.statistics.` on the live qdbd 3.15.0.dev0, among 328 keys
  (`qdbsh direct_prefix_get` against `127.0.0.1:2836`, 2026-11-04).
- A direct statistics read works on the secure cluster as `test-user`
  (`qdbsh direct_int_get $qdb.statistics.startup_epoch` against
  `127.0.0.1:2838`, 2026-11-05). `test-user` is a superuser (`users.cfg`).
- The test clusters listen on `127.0.0.1:2836` and `127.0.0.1:2838`
  (`internal/qdbtest/qdbtest.go:25-26`, the qdbd `-a` flags of
  `scripts/tests/setup/start-services.sh:101`, `:118`). Each has one node.
- The Go 1.27 mux matches a `{id}` wildcard against one escaped path
  segment, so `127.0.0.1:2836` reaches `r.PathValue("id")` unchanged
  (`net/http/routing_tree.go:154-213`, `net/http/pattern.go:199-206`).
- `ClusterStats` names the pool snapshot and `Cluster.Stats` answers it
  (`internal/qdb/cluster.go:867-887`). A snapshot type is named `FooStats`
  (`internal/AGENTS.md`, Code).
- `go mod vendor` overwrites `vendor/github.com/duckdb/duckdb-go-bindings/alloc.go`,
  whose hand patch the re-vendoring commit reapplies (`internal/AGENTS.md`,
  Building).

## Design

### `internal/qdb/cluster.go`

Two constants join the `Operation` block (`cluster.go:397-410`). The
block's comment stays as it is, because it covers the new constants.

```go
const (
	// ... the existing constants
	OpStatistics     Operation = "statistics"
	OpNodeStatistics Operation = "node_statistics"
)
```

### `internal/qdb/statistics.go` (new)

The file reads top to bottom: the type, the sentinels, the classifier,
then the two methods.

```go
// NodeStats is one node's statistics as the binding reads them under
// $qdb.statistics., with the binding's JSON tags. [statistics.go:15-52]
type NodeStats qdbapi.Statistics
	// style: doc comment alone
	// because: a defined type keeps internal/httpapi off the binding's
	// import, and the tags travel with the struct type [Rationale: own type]
```

```go
// ErrNodeNotFound means no endpoint of the cluster has the address
// asked for. [Rationale: endpoint check]
var ErrNodeNotFound = errors.New("no such node")

// ErrNodeUnreachable means the server could not open a direct connection
// to a node. [direct.go:48-51]
var ErrNodeUnreachable = errors.New("node unreachable")
	// style: doc comment alone
	// because: two sentinels like ErrInvalidTag, which the handlers test
	// with errors.Is [table.go:22-25]
```

```go
// nodeError marks the binding's failed direct connection as
// ErrNodeUnreachable and passes every other error through.
func nodeError(err error) error {
	// style: doc comment + inline notes
	// because: one line needs its reason, the test that tells a connect
	// failure from an answer
	//
	// at the errors.As test: DirectConnect is the binding's one statistics
	// path that fails with a plain error rather than an ErrorType [direct.go:48-51, statistics.go:54-99]
	// at the wrap: the sentinel is not IsClusterUnavailable, so one down
	// node does not open the breaker for every route [cluster.go:379-389, Rationale: no breaker]
}
```

```go
// Statistics answers every node's statistics as the user, keyed by the
// node's address. A node it cannot connect to fails the whole call with
// ErrNodeUnreachable. It is retried once on a retryable failure, as a
// read is. [statistics.go:121-140 at 88106da]
func (c *Cluster) Statistics(ctx context.Context, u User) (map[string]NodeStats, error) {
	// style: doc comment alone
	// because: one ObservedCall like Find, with the conversion to
	// NodeStats and nodeError as its only steps [tag.go:42-53]
}
```

```go
// NodeStatistics answers the statistics of the node at addr, an
// <ip>:<port>, as the user. An addr that is no endpoint of the cluster is
// ErrNodeNotFound, and a node it cannot connect to is ErrNodeUnreachable.
// It is retried once on a retryable failure, as a read is.
func (c *Cluster) NodeStatistics(ctx context.Context, u User, addr string) (NodeStats, error) {
	// style: doc comment + overview + inline notes
	// because: the endpoint check must run before the connect, and the
	// code does not say why
	//
	// 1. read the cluster's endpoints on the leased session [cluster.go:101-114 of the binding]
	// 2. refuse an addr no endpoint has [Rationale: endpoint check]
	// 3. read the node's statistics on the same session [statistics.go:109-118 at 88106da]
	//
	// 2. the check comes before the connect, so a caller cannot make the
	//    server dial an address of its choosing [docs/brief.md, Cluster binding]
	// 3. the endpoints and the read share one session, because one
	//    checkout per request is what the pool and the budget are built
	//    around [table.go:306-307]
}
```

### `internal/httpapi/cluster.go` (new)

```go
// clusterPath is the route of the cluster's statistics.
const clusterPath = "/api/v2/cluster"
```

```go
// nodeResponse is one node as both cluster routes answer it: the
// binding's statistics with the node's address as id.
type nodeResponse struct {
	ID string `json:"id"`
	qdb.NodeStats
}
	// style: doc comment + inline notes
	// because: the embedding is the line a reader stops at
	//
	// at the embedded field: encoding/json writes the embedded struct's
	// fields beside id, so a node is one flat object [Rationale: binding struct as is]
```

```go
// clusterResponse is the cluster route's body: every node, sorted by id.
type clusterResponse struct {
	Nodes []nodeResponse `json:"nodes"`
}
	// style: doc comment alone
	// because: the list-body rule says why the array sits under a key, and
	// tablesResponse already carries that reason [tables.go:134-136]
```

```go
// writeNodeError answers a statistics failure: 404 for ErrNodeNotFound,
// 503 for ErrNodeUnreachable, and writeClusterError at 400 for the rest.
func writeNodeError(ctx context.Context, w http.ResponseWriter, err error) {
	// style: doc comment + inline notes
	// because: the 503 bypasses writeClusterError, and a reader would ask
	// why it carries no Retry-After
	//
	// at the 503: no Retry-After, because nothing here knows when the node
	// returns [Rationale: 503 for a down node]
	// at the 404 and the 503: both are refined in the handler and never in
	// writeClusterError [internal/httpapi/AGENTS.md, Handlers]
}
```

```go
// handleCluster answers every node's statistics, sorted by address.
func handleCluster(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + inline notes
	// because: one line needs its reason, the sort
	//
	// at the sort: the binding answers a map, so the handler sorts by
	// address to answer the same order on every call [Rationale: array of nodes]
}
```

```go
// handleNode answers the statistics of the node whose address is the
// path's id.
func handleNode(w http.ResponseWriter, r *http.Request)
	// style: doc comment alone
	// because: it reads the id, calls NodeStatistics and writes, with the
	// errors in writeNodeError
```

```go
// registerClusterRoutes mounts the two cluster routes behind the bearer
// edge.
func registerClusterRoutes(mux *http.ServeMux)
	// style: doc comment alone
	// because: handleData, withCompression and requireBearer as
	// registerTagRoutes uses them [tags.go:74-80]
```

### `internal/httpapi/httpapi.go`

`NewHandler` calls `registerClusterRoutes(mux)` after `registerTagRoutes`
(`httpapi.go:114-115`). Its comment block does not list the routes, so it
stays as it is.

### `internal/httpapi/cluster_test.go` (new)

```go
// cluster gets every node's statistics.
func (s server) cluster() *httptest.ResponseRecorder

// node gets the statistics of the node at addr.
func (s server) node(addr string) *httptest.ResponseRecorder
	// style: doc comment alone
	// because: send with the bearer, as tags_test.go's helpers do [tags_test.go:11-24]
```

### `internal/httpapi/errors_test.go`

Rows join the `rows` map under a `// the cluster` heading, after the tag
rows (`errors_test.go:107-112`):

```go
// the cluster
"cluster: no token":      {GET, clusterPath, no token, 401, "Bearer"}
"node: no token":         {GET, clusterPath + "/nodes/127.0.0.1:2836", no token, 401, "Bearer"}
"node: no such node":     {GET, clusterPath + "/nodes/127.0.0.1:1", bearer, 404, ""}
	// at the no-such-node row: port 1 is no endpoint of either test
	// cluster, so the route refuses it before any connect [Rationale: endpoint check]
```

Two rows join the secure-anonymous map (`errors_test.go:150-161`):
`"cluster: anonymous on the secure cluster"` and
`"node: anonymous on the secure cluster"`, each 401 `invalid_token`.

### `internal/httpapi/roundtrip_test.go`

A step 9 follows step 8 in `TestRoundtrip`:

```go
// 9. the cluster answers exactly the fixture's one node, by its address,
// and the node route answers that node. No statistic is compared,
// because the counters move between two reads [Rationale: no value compared]
	// addr is the fixture's URI without its qdb:// scheme [qdbtest.go:25-26, cluster.go:73-76]
	// cluster(): 200, the ids of nodes equal [addr]
	// node(addr): 200, id equals addr
```

The test's top comment lists the steps
(`internal/httpapi/AGENTS.md`, Tests, describes the round trip), so the
build rewrites that list whole with step 9 in it.

### `docs/e2e.md`

The paragraph that starts "An endpoint lands with its step in the round
trip" (`docs/e2e.md:265-267`) gains:

```
- The cluster and node routes have no step in the driver. `TestRoundtrip`
  checks their status and the node's address and compares no statistic,
  because the counters move between two reads (owner, 2026-11-05).
```

### `internal/AGENTS.md`, Code

A bullet after the tag bullet (`internal/AGENTS.md:139-146`):

```
- The cluster's statistics are `Cluster.Statistics` (every node, keyed by
  address) and `Cluster.NodeStatistics` (one node), over the binding's
  direct connections to each node, read as `NodeStats`.
- NodeStatistics checks the address against the cluster's endpoints before
  it connects (ErrNodeNotFound), so a caller cannot make the server dial an
  address of its choosing.
- A failed direct connection is ErrNodeUnreachable, which does not feed
  the breaker, so one down node does not fail every route.
```

### `internal/httpapi/AGENTS.md`, Handlers

A bullet after the tag routes (`internal/httpapi/AGENTS.md:51-61`):

```
- The cluster routes are `GET /api/v2/cluster`, `{"nodes":[...]}` sorted by
  id, and `GET /api/v2/cluster/nodes/{id}`, one node. A node is the
  binding's statistics as its JSON tags spell them, with its <ip>:<port>
  as `id`.
- An unknown address is 404, a node the server cannot connect to is 503
  without Retry-After, and the rest goes through writeClusterError at 400.
- qdbd's permissions decide who may read a node's statistics, and the
  server adds no authorization of its own, so a refusal answers as
  writeClusterError maps the binding's error (owner, 2026-11-05).
```

### `docs/brief.md`, /api/v2 endpoint sketch

The two cluster rows (`docs/brief.md:566-568`) become:

```
GET    /api/v2/cluster             {"nodes":[...]}: each node's statistics as the binding reads them, its
                                   <ip>:<port> as id, sorted by id; uncached; 503 when a node cannot be read; M8
GET    /api/v2/cluster/nodes/{id}  one node's object, {id} its <ip>:<port>; 404 outside the cluster's endpoints; M8
```

## Rationale

| decision                                                                     | why                                                                                             | rejected, and why                                                                                                                                          | gained                                                      | given up                                                                                          | settled by                                        |
| ---------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| a node is the binding's `Statistics` struct as its JSON tags spell it        | no mapping code                                                                                 | our own struct of the same 22 fields: a mapping function to keep in step with the binding                                                                  | no code between the binding and the wire                    | a field the binding adds or renames changes the v2 wire, and the dotted `chord.node_id` key stays | owner, 2026-11-05                                 |
| `NodeStats` is a defined type over the binding's struct                      | `internal/httpapi` names no binding type, as it names no binding error                          | a type alias, which leaves the binding's name in every signature                                                                                           | the binding stays behind `internal/qdb`                     | one conversion per node                                                                           | proposal                                          |
| the cluster body is `{"nodes":[...]}`, each with `id`, sorted by id          | the list-body rule, and each entry carries the id the node route takes                          | nodes plus a separate endpoint list (the brief's sketch): the address twice; an object keyed by address: no stable order, and it breaks the list-body rule | one shape for a node on both routes                         | a client scans for a node                                                                         | owner, 2026-11-05                                 |
| a node that cannot be read fails the whole cluster route with 503            | the binding's own `Statistics` contract, with no untested branch                                | each node answering for itself: the binding's `Statistics` unused, and a partial path no single-node test cluster can reach                                | one call, one outcome                                       | a down node hides the healthy ones                                                                | owner, 2026-11-05                                 |
| the 503 does not feed the breaker                                            | the breaker gates every route of the cluster, and an operator polling a down node would open it | classifying the connect failure as `IsClusterUnavailable`: one down node fails the data plane                                                              | the data plane stays up beside a down node                  | the breaker never hears of a down node from these routes                                          | proposal                                          |
| the 503 carries no `Retry-After`                                             | nothing in the server knows when a node returns                                                 | the breaker's remaining time: the breaker is not open                                                                                                      | an honest header                                            | none                                                                                              | proposal                                          |
| the node route checks `{id}` against the endpoints before it connects        | a caller must not choose the server's outbound target (`docs/brief.md`, Cluster binding)        | passing `{id}` to `NodeStatistics` as is: the server dials any address a caller names, and an unknown one answers 400                                      | a 404 for an unknown node, and no outbound connection to it | one more C API call per request                                                                   | proposal                                          |
| both calls run under `WithReadRetry` and their own `Operation`               | they are reads, timed like every other leased call                                              | no retry: a retryable failure would surface where every other read retries                                                                                 | one retry on a bad session                                  | none                                                                                              | `internal/AGENTS.md`, Logging and metrics         |
| `TestRoundtrip` checks the status and the address, and compares no statistic | statistics move between two reads and are not worth pinning                                     | comparing each answer with the other, or with qdbsh: flaky                                                                                                 | a step that cannot flake                                    | the values are unchecked                                                                          | owner, 2026-11-05                                 |
| the e2e driver gains no step                                                 | as above, and the Go twin already exercises both routes                                         | a driver step checking status and address                                                                                                                  | no change to the driver                                     | the built binary's cluster routes are not driven over curl                                        | owner, 2026-11-05                                 |
| qdbd's permissions decide who may read the statistics                        | the server relies on qdbd's permission system and implements no authorization layer of its own  | a check in the server, or a test with a non-superuser on the secure cluster                                                                                | no second permission model                                  | a refusal's status is whatever ADR-0010 maps the binding's error to                               | owner, 2026-11-05                                 |
| `go.mod` points at `88106da` until #131 merges, then at its squash commit    | the fix is in review upstream, and the owner chose the PR head for now                          | waiting for the merge before seeding                                                                                                                       | the unit builds now                                         | one more vendor commit before merge                                                               | owner, 2026-11-05; `internal/AGENTS.md`, Building |

## Knowledge

- Commit 2 (vendor): no comment. The `alloc.go` patch is restored as
  `internal/AGENTS.md`, Building, prescribes, and the commit names no
  new fact.
- Commit 3 (`Cluster.Statistics`): the doc comments of `NodeStats`,
  `ErrNodeUnreachable`, `nodeError` and `Statistics`. The inline reasons at
  `nodeError` carry the plain-error fact and the breaker row. The constants
  carry no comment.
- Commit 4 (`Cluster.NodeStatistics`): the doc comments of `ErrNodeNotFound`
  and `NodeStatistics`, and its overview and inline reasons. They carry the
  endpoint-check row and the one-session reason.
- Commit 5 (cluster route): `nodeResponse`, `clusterResponse`,
  `writeNodeError` and `handleCluster`, with the embedding note, the
  no-`Retry-After` row and the sort row inline.
- Commit 6 (node route): `handleNode` and `registerClusterRoutes`.
- Commit 7 (error rows): the port-1 note at its row.
- Commit 8 (round trip): the step 9 comment, which carries the
  no-value-compared row, and the rewritten step list.
- Commit 9 (`docs/e2e.md`, a specification row): the no-driver-step row.
- Commit 10 (`internal/AGENTS.md`, `internal/httpapi/AGENTS.md`): the
  statistics core, the endpoint check and the no-breaker rule, because they
  span the files of `internal/qdb`, and the wire shape and statuses of the
  routes, because they span the handlers and the tests. The httpapi
  bullet also carries the row that qdbd's permissions decide who may read
  the statistics.
- Commit 11 (`docs/brief.md`): the sketch rows. The brief owns the
  endpoint sketch (`docs/AGENTS.md`, What lives where).
- `plan only`: the rejected own-struct alternative, the rejected
  per-node-error alternative, the rejected driver step and the rejected
  qdbsh comparison. They are owner answers in this unit's thread, and no
  reader of the code would propose them again from the code alone. The
  `88106da` pin is `plan only` as well, because commit 12 replaces it.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code, and reconciles
the code against this plan before the build-stage message.

- Before commit 8, the executor checks that both test clusters answer
  their own `-a` address from `Cluster().Endpoints()`. If an endpoint
  differs from the fixture URI's host and port, step 9 derives `addr` from
  the cluster route's answer instead, and the step's comment says so.
- Commit 12 needs #131's squash commit on qdb-api-go `master`. If #131 has
  not merged when the build reaches it, the build pushes and asks for
  review without it, says so in the build-stage message, and lands it
  before `/rr-merge` with a fresh build.

## Commits

1. `docs(plan): cluster-statistics-plan.md, the cluster and node statistics routes`
2. `build(vendor): qdb-api-go at 88106da, which reads one node's statistics by address`
3. `feat(qdb): Cluster.Statistics answers every node's statistics, ErrNodeUnreachable on a failed connect`
4. `feat(qdb): Cluster.NodeStatistics answers one node's statistics, ErrNodeNotFound outside the endpoints`
5. `feat(httpapi): GET /api/v2/cluster answers the nodes sorted by address, 503 on an unreachable node`
6. `feat(httpapi): GET /api/v2/cluster/nodes/{id} answers one node, 404 on an unknown address`
7. `test(httpapi): the error rows cover the cluster routes`
8. `test(httpapi): the round trip reads the cluster and its node by address`
9. `docs(e2e): the cluster routes have a step in the Go round trip only`
10. `docs(agents): the internal and httpapi agents files name the statistics core and the cluster routes`
11. `docs(brief): the cluster rows answer an array of nodes, each the binding's statistics with its address`
12. `build(vendor): qdb-api-go at the squash commit of #131 on master`
13. `/doc-discipline all` over `internal/qdb/statistics.go`, `internal/httpapi/cluster.go`,
    `internal/httpapi/cluster_test.go`, `internal/httpapi/errors_test.go`,
    `internal/httpapi/roundtrip_test.go` and the documents of commits 9-11.
    Its fixes land as further `docs(...)` commits.
14. `/doc-discipline check docs/cluster-statistics-plan.md`. Each finding is
    fixed in a further `docs(...)` commit before the next step.
15. `docs(log): cluster-statistics-plan.md deleted, facts moved to internal/AGENTS.md, internal/httpapi/AGENTS.md, docs/e2e.md, docs/brief.md and the comments of internal/qdb and internal/httpapi`.
    Current state marks M8 done, drops Next item 1 and the M8 criteria,
    and names M9 as next. A dated entry "M8 closed" says the round trip
    reads the cluster and one node by address and compares no statistic
    (owner, 2026-11-05), beside the entry for the plan's deletion.
16. Verify: push `sc-19567/rr-cluster-statistics`, build its head in
    Buildkite, wait for the result. Green: report the build number. Red: fix
    with further small commits on this branch, push, build again.

## Open questions and recommendations

none
