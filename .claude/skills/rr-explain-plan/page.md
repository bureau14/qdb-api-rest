# The page

One self-contained HTML page, figure-first, in the `visual-explainer`
kit with the Plannotator theme tokens from
`plannotator-visual-explainer/references/theme-override.md`, and its
`<head>` carries `<meta name="plannotator-theme" content="host">` so
the annotation viewer themes it. Register `blueprint`. Captions and
labels follow the root `AGENTS.md`, Prose, and are ASCII. Every
figure draws the mechanism and names a verb on every edge. A
component is a box with its path in mono; a package is a dashed
boundary with the package path as its tag.

## Origin in form

Every constraint is drawn where the thing it constrains is drawn, and
its origin is in the element's form, never in color alone:

- `owner`: a solid frame with a small "owner, <date>" tag.
- `dependency`: a hatched frame with the module path as its tag, so
  it reads as something this repository does not own.
- `existing code`: a dashed frame with the `path:line` tag, the
  accent of the page, and the one kind of element that may carry the
  `.is-key` halo. There is at most one halo per figure, on the
  constraint the figure is about.
- `convention`: a dotted frame with the document name as its tag.

A plan claim that differs from the tree is one element with two
labels, "plan:" and "tree:", and nothing else said about it.

## Drawing

The renderer rejects a label wider than its box and a label that
crosses a boundary border, and a landscape of twenty boxes fails
those checks many times when drawn by hand. Generate the SVG of
every figure with more than six boxes from a short script that sizes
each box from its longest label, keeps gutters wide enough for a
short verb, and rotates the label on a vertical gutter. Keep the
script outside the repository next to the page.

## Sections, in order

1. **Header.** Eyebrow: the branch name. Title: the one sentence the
   owner must leave with, which is what the plan changes and what
   forces its shape, under about twenty words. Lead: one sentence on
   where the unit sits, the milestone and the Next item it answers,
   from `docs/log.md`. No stat strip, no timeline.
2. **Landscape.** Fig. 1: the scoped set as it is in the tree today.
   Packages as boundaries, the components the plan touches as boxes,
   the import edges and the call edges between them with their verbs.
   Components the owner marked as known are one labeled box with no
   interior. Nothing from the plan is on this figure.
3. **The change on the landscape.** Fig. 2: Fig. 1 again with the
   plan's whole delta drawn on it, as one static figure. Added
   elements are drawn with a thicker outline and a "+" tag, removed
   ones struck through with a "-" tag, changed ones with a "~" tag,
   and each tag carries the number of the commit that makes it. Each
   constraint from the table is drawn on the element it constrains,
   in its origin's form, with its tag. This figure says where the
   plan lands and nothing about what a commit does; a replay of
   commits over this figure was tried and the owner could not read
   what a commit did from it, so the commit cards below carry that.
4. **Commit cards.** One per commit the owner chose in the scoping
   round, in landing order, each three parts under one heading that
   is the commit's subject in plain words:
   - the mechanism, Fig. N: the one thing the commit changes, drawn
     large and alone, before on the left and after on the right, at
     the level of the loop, the call or the type, never the
     landscape. For a loop, the two loops with their stride and
     their check; for a new path, its hops; for a type, its methods
     before and after. The constraint that shapes the change sits on
     the after side in its origin's form;
   - the ledger: a `.ve-table` with one row per file the commit
     touches and three columns, goes, comes, stays, naming functions,
     types, constants and document rows, so the size of the commit
     is countable;
   - the sketch: the plan's Design sketch of the commit's main
     function, as a code block, with the `style:` and `because:`
     lines and the claims as they stand in the plan. The code block
     shows the owner the size and shape of what will exist. A commit
     with no sketch in the plan has no third part.
     The caption names the Rationale row the commit executes and says
     what the owner can judge from the card: the size, the ownership
     of the loop, the hop that is new.
5. **A request, before and after.** Fig. 3: a stepper that walks one
   request through the landscape as the tree runs it today, one step
   per hop, and Fig. 4: the same request after the plan, through the
   new elements. When the plan adds a second path (a new listener, a
   new verb), Fig. 4 walks the new path and the caption names the
   existing path it mirrors. Each step's label says what the hop does
   and which constraint shapes it.
6. **Cases.** Small multiples when the plan fixes a set of outcomes: a
   precedence of error codes, a set of body shapes, a set of empty
   cases. One mini figure per case, the same drawing each time, the
   case's difference marked. Ordered as the plan orders them.
7. **Traced decisions.** One figure per decision the owner chose in
   the scoping round: the element the decision shapes, the edges it
   forces elsewhere in the tree, and the rejected alternative drawn
   beside it as a ghost with the edges it would have forced. The
   caption states the Rationale row's "gained" and "given up" as two
   sentences, without a verdict.
8. **Hints.** The closing list. One line per hint: the figure it is
   on, the thing, and why the owner would look at it. A hint is one
   of five kinds, named in the line:
   - expensive to reverse: a wire shape, a public signature, a
     config key, a storage layout, a vendored package;
   - a rule the code must now keep, from the Rationale's "given up";
   - a fact the plan marks unverified;
   - a component outside the plan's scope that the change reaches;
   - an existing-code constraint the plan accepted rather than
     removed.
     Every hint is also a marker on the figure where the thing is
     drawn. The fifth kind is the explicit invitation to overrule, and
     its line says what the plan does because of the constraint.
9. **Sources.** Every `path:line` the figures cite, as the kit's
   sources section.

Drop a section whose content the plan does not have (no cases, no
traced decision chosen, no commit card chosen); never add a section. Every figure has a
caption that states its claim in one sentence.

## The flight-statement plan as the example

Correct, for `docs/flight-statement-plan.md`: the title says the
Flight listener answers a statement through the HTTP query path's
core, and the gRPC message limit plus the one-batch encoders are what
shape it. Fig. 1 draws `internal/flightsql` (the server, the auth
interceptors, the login), `internal/query` (`Run`), `internal/qdb`
(`Cluster.Query`, `Cluster.Duck`, `Batches`), `internal/olap` (the
instance's lent batches), `internal/encoding` (`Encoder` with its two
methods, the Arrow encoder's slicing loop), `internal/model`, and
`internal/httpapi`'s query handler and `writeClusterError`, with the
import edges `encoding -> model`, `qdb -> model`, `qdb -> olap`, and
the call edges from the handler through `Run` to the cluster and the
encoder. The vendored arrow-go router is a hatched box outside every
boundary. Fig. 2 adds `statement.go` and `status.go` to `flightsql`,
`slices` to `qdb`, `BatchRows` to `model`, strikes `Encode`,
`chunkRows`, `checkChunk`, `emptyBatch` and `readBatchRows`, and
draws on them: the gRPC receive limit as a hatched constraint on the
edge into the router, the `encoding`-never-imports-`qdb` edge as the
dashed, haloed existing-code constraint on `BatchRows`'s box, the
lending rule of `Batches` as a dashed constraint on `slices`, and the
owner's "one batch size" as a solid frame on `BatchRows`. Fig. 3
steps an HTTP query today: handler, `Run`, `Cluster.Query`'s one
batch, the Arrow encoder slicing it. Fig. 4 steps a Flight `DoGet`
after the plan: the ticket decoded, the claims read, `Run`, the
slices, the writer opened on the first batch, each batch written,
the close. The commit card for commit 5 draws the CSV encoder's row
loop with its `checkChunk` call every 65536 rows on the left and the
per-batch loop with one ctx check at its top on the right, the
owner's one-batch-size decision on the after side; its ledger has
one row per encoder file and one for the e2e tool, with `Encode`,
`checkChunk`, `chunkRows` and `numRows` under goes and the `one`
helper under comes; its sketch is the plan's `Encoder` interface.
The card for commit 8 draws the four hops of `DoGet` after the plan
beside the base server's `UNIMPLEMENTED` answer today, the lending
rule of `Batches` on the writer's hop, and its sketch is the plan's
`DoGet`. The cases are the seven outcomes of `statusOf`, in
precedence order, each a mini figure of the same path with the
failing hop marked. The hints include `RetryInfo` as a vendored
package and an unverified driver behavior, the JSON body of a large
native result becoming several results as a reach outside the
Flight scope, and the import graph as the existing-code constraint
that placed the constant.

Incorrect: Fig. 1 draws `Cluster.Query` yielding slices because the
plan says it will. Or: the page opens with a stat strip of "6
packages, 11 commits, 22 facts". Or: commits 4 and 5 are one replay
step of Fig. 2 captioned "the Arrow slicing loop, the empty batch,
Encode and the per-row ctx stride go", so the owner sees four names
disappear and not what the encoders do afterwards. Or: the cases are a table of seven
sentences. Or: the hints list says the `DoGet` override is "the
simpler choice". Or: the `encoding`-never-imports-`qdb` edge is drawn
as a plain line, so the owner cannot tell it is the thing that put
the constant where it is.
