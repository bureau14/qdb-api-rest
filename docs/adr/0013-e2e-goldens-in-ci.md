# ADR-0013: e2e goldens run in Buildkite; measured numbers are the bench's

Status: accepted
Date: 2026-09-17

## Context

Three activities shared the name "e2e": checking that the built binary
returns the right response, checking that it is a drop-in for the old
server, and measuring time to first byte, memory and wall clock. The
first two have a yes/no answer; the third produces a number somebody
reads. Filing them together put performance numbers into milestone
criteria of a harness that has no result files, made performance
budgets a CI gate on shared agents, and left the harness out of
Buildkite altogether, so no milestone closed on CI evidence of the
binary as a client sees it.

The legacy goldens were defined as "what the old server said". v1
deviates from the old server deliberately in a few places (brief,
Compatibility contract, "Deliberate deviations"), and v2 has no old
server to capture from, so that definition neither holds for v1 nor
extends to v2.

## Decision

1. **Four layers, one question each.** Go tests, property tests
   included, answer whether the logic is right for any input. The e2e
   goldens answer whether the built binary, driven over HTTP, returns
   exactly the audited response. The e2e stress answers whether the
   binary behaves under load and across a drain, asserted as behaviour,
   never as a timing. The assessment bench answers how fast, how much
   memory, and whether a real legacy client reads the same data from
   the old and the new server. The first three run in Buildkite on
   every platform and gate; the bench runs on a developer machine and
   gates nothing.
2. **A number is the bench's.** Time to first byte, RSS, throughput and
   wall clock are measured by the bench and live in its result files.
   No milestone criterion, e2e assertion or CI gate is a measured
   number. Performance budgets are not CI gates.
3. **A golden is an audited expected response.** A run somebody looked
   at, judged correct and committed; later runs are compared with it
   byte for byte, with no canonicalization and no tolerance. A v2
   golden is captured from the server under test and audited against an
   independent source. A legacy golden is captured from the old server,
   whose behaviour is the specification. Capture never runs in CI; a
   golden changes only in a reviewed commit.
4. **A deliberate deviation is an overlay file.** The capture from the
   old server stays untouched; a hand-written `body.v1`, `status.v1` or
   `headers.v1` next to it is what the server under test is compared
   with. An overlay exists only for a deviation the brief lists.
5. **Text is compared as bytes, Arrow IPC as decoded content.** JSON,
   NDJSON and CSV bodies are compared byte for byte; gzip after
   decompression. The Arrow format leaves the value of a null slot and
   of padding undefined, and the batch's buffers are the C API's,
   handed through zero-copy, so an Arrow body has no stable bytes.
   Decoding normalizes both: a pure-Go tool in the harness reads the
   stream with `arrow-go`, prints the schema and renders the batches
   through the CSV encoder, and the result is compared byte for byte
   with a small schema golden and with the audited CSV golden of the
   same query. The Arrow case asserts that the binary's Arrow stream
   carries exactly what its audited CSV response carries.
6. **A suite enters CI when it is green.** The legacy suite is a local
   red bar until the wrappers land, then joins the build step.
7. **An endpoint lands with its goldens.** Every milestone's exit
   criteria name the suite that is green in Buildkite.

## Consequences

- Every milestone closes on CI evidence of the binary; the first
  shippable binary is CI-proven as a drop-in at the byte level.
- The old server is needed in two places only, both local operator
  steps: capturing legacy goldens and the bench's old-server runs.
  Replay needs the committed goldens and nothing else.
- Byte-shape compatibility is CI's; semantic compatibility at full
  size through a real client stays a by-product of the bench run that
  measures the same pair.
- The list of v1 deviations is a directory listing of `.v1` files, and
  each one is a diff between two files; the comparator is `cmp`.
- CI loads the dataset on every platform; e2e cases bound their own
  result size, and no e2e case selects the whole table.
- A performance regression is caught by a person running the bench, not
  by a build. A local bench threshold is an addition the bench can ask
  for.
- The harness builds one Go tool; it imports `internal/encoding` and
  `arrow-go` only, so it needs no cgo and builds on every platform.
- The full-size semantic check of the Arrow path through a real client
  (pyarrow into DataFrames, fingerprinted against the native client) is
  the bench's `http-arrow@new-rest` run.
- An encoder change that alters bytes fails the goldens by design;
  recapturing is an audited, reviewed step.

## Alternatives rejected

| Alternative                                       | Why not                                                                                                                                     |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Performance budgets as CI gates                   | shared agents make a timing bound flaky or meaningless; materialization puts most of it outside this binary                                 |
| Numbers recorded by the e2e harness               | a second home for what the bench measures; nothing gates on them                                                                            |
| A comparator rule per deviation                   | hides the deviation in shell code and grows with every one                                                                                  |
| Editing a captured body in place                  | the selfcheck against the old server stops proving the capture                                                                              |
| A sha256 in place of a large expected body        | a failure has nothing to diff against                                                                                                       |
| A canonicalizing or tolerance comparator          | output is deterministic; an unexpected byte is a bug worth seeing                                                                           |
| Arrow IPC compared as raw bytes                   | the format leaves null slots and padding undefined; stable bytes would be an accident of one C API release                                  |
| Arrow IPC left to the property test alone         | the binary's negotiation, multi-batch stream and compression, over real nulls, would never be driven from outside                           |
| pyarrow or pandas as the decoder in the harness   | no pyarrow wheels on FreeBSD, a venv on every agent, Python in the permanent path; pandas turns a nullable int64 into float64               |
| A decoder independent of the CSV encoder          | a second rendering needs a second audited golden; the CSV golden is audited on its own, so a renderer bug cannot hide behind the comparison |
| e2e kept out of CI until the resilience milestone | every earlier milestone, the first shippable binary included, would close on local evidence only                                            |
| The red legacy suite in CI before the wrappers    | a permanently red step teaches everyone to ignore the build                                                                                 |
