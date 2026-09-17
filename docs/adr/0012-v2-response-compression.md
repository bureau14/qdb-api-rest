# ADR-0012: v2 response compression

Status: accepted
Date: 2026-09-16

## Context

The brief fixes that response compression is negotiated via
`Accept-Encoding`, identity by default and never forced, in zstd and
gzip (`docs/brief.md`, Goals and Data plane), and leaves the wire rule
to an ADR. Every v2 endpoint inherits the rule, and the v1 layer
(ADR-0007) wraps v2 handlers, so the mechanism must be one a wrapper
can apply with a different matcher. zstd is already vendored and linked:
`arrow-go`'s IPC package imports `klauspost/compress/zstd`, so offering
it costs one import and no vendored bytes.

## Decision

1. **`Accept-Encoding` selects the coding**: `gzip`, `zstd` or
   `identity`. The listed codings are read in the client's order and
   the first one the server produces wins; an absent header, no match
   and `identity` all mean identity. `q` weights are not read: a client
   that wants a coding names it, the rule `Accept` follows (ADR-0010).
2. **Compression is a writer below the encoders**, applied per route
   like the bearer middleware: the login and the query. The probes stay
   outside; their bodies are empty. The
   encoders are untouched and Arrow IPC's in-format buffer compression
   stays off.
3. **A compressed response is labelled at its first body byte**:
   `Content-Encoding` is set and the compressor opened when the handler
   writes its first byte; a status without a body goes out as written,
   unlabelled and empty. Problem bodies compress like any other. Every
   response of a compressing route carries `Vary: Accept-Encoding`.
4. **Fastest level, one compressor per response, no knob**: gzip at
   `BestSpeed`, zstd at `SpeedFastest` with encoder concurrency one. A
   gateway pays CPU per byte on every compressed response; the bytes
   saved are the WAN client's gain.

## Consequences

- Every later v2 route opts in by wrapping its handler; the rule never
  grows a second shape. M3's v1 wart (substring `gzip`, every route)
  exports what it needs of the writer when it exists.
- The access line counts wire bytes: compression sits inside the
  request logger.
- A level knob or pooled compressors are additions the bench can ask
  for without a wire change.
- The brief's open question on vendoring `klauspost/compress` is
  answered by the tree.

## Alternatives rejected

| Alternative                          | Why not                                                                          |
| ------------------------------------ | -------------------------------------------------------------------------------- |
| Server preference (zstd over gzip)   | one negotiation rule for `Accept` and `Accept-Encoding`; curl's order picks gzip |
| `q` weights                          | not read for `Accept` either; a client names what it wants                       |
| Library default levels; a level knob | CPU per byte is the gateway's cost; no client has asked for a knob               |
| A `sync.Pool` of compressors         | package-level state for an allocation the bench has not measured                 |
| Compressing the mux, probes included | an empty probe body under `Content-Encoding` is an empty frame nobody asked for  |
| Labelling at `WriteHeader`           | a status without a body would claim a coding it does not carry                   |
| zstd in M4                           | already vendored and linked; deferring it costs more than the one import         |
