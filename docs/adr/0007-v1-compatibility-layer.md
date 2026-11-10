# ADR-0007: v1 compatibility layer: v2 first, v1 wraps v2, one package

Status: accepted
Date: 2026-09-02

## Context

The brief's Compatibility contract freezes the old server's unversioned
API as v1, served forever, warts included, answering as the old
server's code does except the deviations the Compatibility contract
lists, and mints everything new under `/api/v2/*`. The two surfaces
serve different purposes. v1 exists so an unchanged client keeps
working, and v2 is the product. Two rules follow from that and were
taken separately. A v1 route carries no parallel implementation and
wraps its v2 counterpart (owner decision, 2026-08-31), and v1 code
stays out of current-protocol code.

Both rules are unsatisfiable while the v1 surface is built first. A
wrapper needs something to wrap. With no v2 core in the tree, a v1
route can only be a direct implementation, with its own query
execution, its own JSON encoder carrying the key order and the null
typing, and its own token extraction, which is later either unwound
or, worse, becomes the seed the v2 core grows out of, carrying the
warts into the current protocol. Isolation is impossible for the same
reason. When the only code in the tree is v1 code, "v1" and "the
codebase" name the same thing and there is no seam to keep. The direct
`/api/v1/query` implementation begun under the v1-first order
demonstrated this and was discarded.

The early-drop-in argument (ship the compatible binary first to
de-risk the compatibility story) does not need the v1 code first
either. Compatibility is checked by a script run by hand, which starts
the old server built from the tag `archive/go-swagger-master` and the
new one on the same cluster, sends each case to both and compares the
answers (ADR-0015). The check
holds the contract whenever the code that satisfies it is written.

## Decision

1. **v2 first.** The v2 data plane (query execution core, encoders,
   auth endpoints) is built before any v1 route. No v1 route is written
   before the v2 counterpart it wraps exists. The milestone order in
   the brief (v2 query before drop-in compat) follows from this, not
   the reverse.
2. **v1 wraps v2.** Every v1 route wraps its v2 endpoint. It builds
   the v2 request from the v1 one, calls the v2 endpoint in process
   through the v2 router, and converts the answer into the v1 wire
   shape, which is the key order, the null typing, the error bodies,
   the `find` prefix, and the header and `?token=` extraction. The
   translation cost, an in-process request per v1 call and the v2 body
   parsed again, is accepted, because duplicated logic is the greater
   cost (owner, 2026-11-05). A separate v1 implementation is justified
   only when wrapping is impossible or at least doubles the route's
   measured cost.
3. **One package with its own builder.** v1 code lives in
   `internal/httpapi/v1`, a package that imports `internal/httpapi`.
   Its builder takes the v2 router and hands it every request that is
   not a v1 route. The binary's entry point nests the two and wraps the
   result in the request logging once, so every request writes one
   access line whichever router answers it. `internal/httpapi` never
   imports the v1 package, so an import cycle makes the wrap direction
   a compiler fact rather than a convention. Inside the package, names
   say v1, and outside it, nothing knows a wart exists. The package
   carries its own `AGENTS.md` for the v1 wire facts.

## Consequences

- v2 shapes are chosen on v2's merits, and the v1 wrapper pays whatever
  translation that costs. The v2 JSON encoder never orders keys or
  types a column for a v1 client.
- The v1 login wraps the `POST /api/v2/auth/login` endpoint, so it
  exists only once that endpoint does.
- Retiring or auditing the v1 surface is one directory.
- The entry point nests the v2 router in the v1 router and wraps the
  result in the request logging. That is composition rather than the
  state the context carries (ADR-0002), and the entry point knows no
  v1 path.
- A request to a v1 path under a method the v1 router does not serve
  reaches the v2 router, which answers 404 rather than 405.

## Alternatives rejected

| Alternative                                                             | Why not                                                                                                                                          |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| v1 first, to de-risk the drop-in early                                  | forces a direct implementation that is unwound or seeds v2 with warts, leaves no seam to isolate, and was tried and discarded                    |
| Parallel v1 and v2 implementations                                      | two query paths, two encoders and two auth extractions drifting apart, with warts in two places                                                  |
| `v1_*.go` files inside `internal/httpapi`                               | visible to a reader and invisible to the compiler, so a bare helper can grow v1 behaviour and nothing forbids the reverse dependency             |
| A top-level `internal/v1` package                                       | only the HTTP plane has a v1 surface (Flight SQL and DuckDB are new), and a top-level package suggests a cross-cutting layer that does not exist |
| An `internal/httpapi/v2` package for the current protocol               | the current protocol is the package, and a `v2` path element is confusable with a module major-version suffix                                    |
| The router imports the v1 package and registers itself                  | makes `internal/httpapi` depend on v1, and the wrap direction becomes a convention again                                                         |
| The v2 builder takes route callbacks from the v1 package                | the owner found it not Go-like (2026-11-05)                                                                                                      |
| v1 calls exported v2 functions (`Cluster.Authenticate`, `Tokens.Issue`) | the v1 package would repeat the v2 handler's body parsing and error mapping                                                                      |
| A root mux in the entry point listing the v1 paths and aliases          | the entry point would carry v1 knowledge                                                                                                         |
| v1 serves a separate router beside v2                                   | a v1 request would miss the access line or write it twice                                                                                        |
