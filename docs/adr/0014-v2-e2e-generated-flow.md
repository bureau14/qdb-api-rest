# ADR-0014: the v2 e2e is a generated roundtrip flow, not a golden suite

Status: accepted
Date: 2026-09-18

## Context

ADR-0013 defined the e2e layer as goldens: an audited expected response
per case, captured once, compared byte for byte ever after. For v1 that
definition is exact, because the old server is the specification and a
capture from it is the audit. For v2 there is no old server: a v2
golden would be captured from the server under test and audited by an
operator against qdbsh output or an ADR, case by case, on every wire
change. The first slice of that suite came to ten cases, an operator
capture cycle, an archive layout for large bodies, and error goldens
that repeated rows the Go tests in `internal/httpapi` already pin. The
maintenance was out of proportion to the question the layer answers:
does the built binary, driven like a client, work.

The brief already has an ingest/query roundtrip as a property test
(Testing doctrine 1) and the harness already has an import/export
roundtrip compared with `cmp` (`make verify-dataset`): the expected
value of a roundtrip is its input, and an input that is generated needs
no audit.

## Decision

1. **The v2 e2e is one flow**, run on each cluster against the built
   binary: login, create a table, query it empty, ingest generated rows
   through every input format, query them back in every format and
   content coding. It proves the basic flow; it does not pin the
   surface.
2. **The expected value is the generated input.** The rows are
   generated in the CSV encoder's dialect; the CSV response is compared
   with them byte for byte; a JSON, NDJSON or Arrow IPC response is
   decoded by a pure-Go tool in the harness and rendered through the
   same CSV encoder, then compared with the same file (ADR-0013 5's
   Arrow rule, applied to every rendered format). A gzip response is
   decompressed first. Nothing is captured, nothing is audited, nothing
   is stored under `tests/e2e/golden/v2/`.
3. **Error rows are Go tests.** The ADR-0010 and ADR-0011 tables are
   pinned in `internal/httpapi`; the e2e layer asserts the happy path
   only. A login is checked by shape (RFC 6749's fields), a query by
   its decoded content, an ingest by the rows that come back.
4. **One Go tool, no cgo**: it generates the rows (every column type,
   nulls, the awkward string, nanosecond timestamps) and decodes the
   rendered formats to CSV. It imports `internal/encoding` and
   `arrow-go` only and builds on every platform.
5. **ADR-0013 3 and 5 apply to v1 only** from this decision on: a v1
   golden is what the old server said. ADR-0013 1, 2, 4, 6 and 7 stand
   for both suites, with "its goldens" read as "its e2e coverage" for
   v2: an endpoint lands with its step in the flow.
6. **The flow needs `POST /api/v2/tables` and
   `POST /api/v2/tables/{name}/rows`**, so they move from the
   exploration and ingestion milestones into a milestone directly after
   M1 (`docs/brief.md`, Milestones). Their wire shapes are ADR-0015's.

## Consequences

- No operator step in the v2 layer: a wire change that breaks the flow
  is a failing build, not a recapture; a deliberate wire change
  changes the tool or the driver in the same commit.
- The CSV path is proven against a source the encoders never touched;
  the other three formats are proven equal to it. A CSV encoder bug
  that hides from the CSV comparison would have to be mirrored by the
  parser that reads the generated CSV; the property tests cover that
  pair independently.
- Two limits of the tree bound the generated data until they lift: CSV
  renders null and the empty string alike, so the generator emits no
  empty string; the batch writer cannot write a null timestamp cell
  until the `qdb-api-go` upstream fix, so timestamp columns carry no
  nulls until then.
- The dataset archive carries no `expected/` directory, and the v2
  layer reads neither `reproduce` nor `seed.sql`: the v1 suite and the
  bench are their remaining readers.
- The e2e layer no longer exercises a large result or a multi-batch
  Arrow stream from outside; the row count of the flow is a Makefile
  variable, and the bench's `http-arrow@new-rest` run is the full-size
  check.

## Alternatives rejected

| Alternative                                          | Why not                                                                                                      |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| The v2 golden suite as planned                       | ten cases, an operator capture cycle and an archive layout to prove a flow a roundtrip proves with no audit  |
| Error goldens (401, 413, 415, 400)                   | the rows are already Go tests; an e2e error case repeats a table without proving a flow                      |
| A renderer per format in the harness                 | a second rendering of JSON, NDJSON and Arrow in shell, kept in sync with the encoders by hand                |
| Row-count checks for JSON and NDJSON                 | a count proves nothing about the cells; the decoded comparison costs one Go tool the harness builds anyway   |
| A checked-in fixture CSV                             | fixed data hides what a generator finds; the generator shares its vocabulary with the property tests         |
| An awk generator                                     | blobs, nanosecond timestamps and Arrow IPC bodies in awk; the Go tool exists for decoding already            |
| Waiting for the exploration and ingestion milestones | the flow would enter CI five milestones late; two small endpoints now are cheaper than a fixture thrown away |
