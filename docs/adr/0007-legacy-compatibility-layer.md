# ADR-0007: Legacy compatibility layer: v2 first, v1 wraps v2, one package

Status: accepted
Date: 2026-09-02

## Context

The brief's Compatibility contract freezes the legacy unversioned API as
v1 -- served forever, warts included, pinned byte-for-byte by the e2e
goldens -- and mints everything new under `/api/v2/*`. The two surfaces
serve different purposes: v1 exists so an unchanged client keeps
working; v2 is the product. Two rules follow from that and were taken
separately: a v1 route carries no parallel implementation and wraps its
v2 counterpart (owner decision, 2026-08-31), and legacy code stays out
of current-protocol code.

Both rules are unsatisfiable while the legacy surface is built first. A
wrapper needs something to wrap: with no v2 core in the tree, a v1 route
can only be a direct implementation -- its own query execution, its own
JSON encoder carrying the sentinel strings and the key order, its own
token extraction -- which is later either unwound or, worse, becomes the
seed the v2 core grows out of, carrying the warts into the current
protocol. Isolation is impossible for the same reason: when the only
code in the tree is legacy code, "legacy" and "the codebase" name the
same thing and there is no seam to keep. The direct `/api/v1/query`
implementation begun under the legacy-first order demonstrated exactly
this and was discarded.

The early-drop-in argument (ship the compatible binary first to de-risk
the compatibility story) is already served by the goldens: the red bar
`make -C tests/e2e test-legacy` exists and stays red until the wrappers
land, so the compatibility contract is enforced regardless of when the
code that satisfies it is written.

## Decision

1. **v2 first.** The v2 data plane -- query execution core, encoders,
   auth endpoints -- is built before any legacy route. No legacy route
   is written before the v2 counterpart it wraps exists; the milestone
   order in the brief (v2 query before drop-in compat) follows from
   this, not the reverse.
2. **v1 wraps v2.** Every v1 route is a wrapper around its v2
   counterpart's core: it parses the legacy request shape, calls the v2
   core, and translates the result into the legacy wire shape -- key
   order, sentinel strings, error bodies, the `find` prefix, the header
   and `?token=` extraction. Translation overhead is an accepted price;
   a separate v1 implementation is justified only when wrapping is
   impossible or at least doubles the route's measured cost.
3. **One package.** Legacy code lives in `internal/httpapi/legacy`, a
   package that imports `internal/httpapi` for the v2 core. The binary's
   entry point composes the two; `internal/httpapi` never imports the
   legacy package, so an import cycle makes the wrap direction a
   compiler fact, not a convention. Inside the package, names say
   legacy; outside it, nothing knows a wart exists. The package carries
   its own `AGENTS.md` for the wire facts the goldens pin.

## Consequences

- v2 shapes are chosen on v2's merits; the v1 wrapper pays whatever
  translation that costs. The v2 JSON encoder never emits a sentinel or
  orders keys for a legacy client.
- The legacy login is a wrapper over `POST /api/v2/auth/login`, so
  it exists only once that endpoint does; until the wrappers land, the
  goldens hold the compatibility contract.
- Retiring or auditing the legacy surface is one directory.
- Composing the legacy routes into the server is the entry point's job;
  how they reach the router is decided when the package is written, and
  it is composition, not the state the context carries (ADR-0002).

## Alternatives rejected

| Alternative                                                | Why not                                                                                                                                          |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Legacy first, to de-risk the drop-in early                 | forces a direct implementation that is unwound or seeds v2 with warts; leaves no seam to isolate; tried and discarded                            |
| Parallel v1 and v2 implementations                         | two query paths, two encoders, two auth extractions drifting apart; warts in two places                                                          |
| `legacy_*.go` files inside `internal/httpapi`              | visible to a reader, invisible to the compiler: a bare helper can grow legacy behaviour and nothing forbids the reverse dependency               |
| A top-level `internal/legacy` package                      | only the HTTP plane has a legacy surface (Flight SQL and DuckDB are new); a top-level package suggests a cross-cutting layer that does not exist |
| An `internal/httpapi/v2` package for the current protocol  | the current protocol is the package; a `v2` path element is confusable with a module major-version suffix                                        |
| The router imports the legacy package and registers itself | makes `internal/httpapi` depend on legacy; the wrap direction becomes a convention again                                                         |
