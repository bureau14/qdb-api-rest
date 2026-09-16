# v2 Response Compression -- Plan

Status: approved. Working plan for the last M1 unit: response compression
negotiated via `Accept-Encoding` on the v2 routes. Deleted when the work
lands; the wire contract moves to an ADR, the handler rules to
`internal/httpapi/AGENTS.md`.

## Approach

- One middleware, `withCompression`, applied per route like
  `requireBearer`: the login and the query. The probes stay outside:
  their bodies are empty and their headers are golden-pinned.
- The middleware reads `Accept-Encoding`, picks a coding, and wraps the
  `ResponseWriter`. `Content-Encoding` and `Vary` are set when the
  handler writes its status; the compressor opens on the first body
  byte, so a response without a body carries no compressed frame and a
  problem body is compressed like any other.
- The encoders are untouched: compression is a writer below them, the
  Arrow IPC in-format compression stays off.
- gzip through the standard library; zstd through
  `klauspost/compress/zstd`, already vendored and linked by
  `arrow-go`'s IPC package, so it costs one import.

## Decision log

| Decision                                                         | Why                                                                                        | Rejected                                        |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------ | ----------------------------------------------- |
| Codings read in the client's order, first supported wins, no `q` | one negotiation rule for `Accept` and `Accept-Encoding` alike (ADR-0010)                   | server preference (zstd over gzip); `q` weights |
| Fastest level for both codings                                   | a gateway pays CPU per byte on every response; the bytes saved are the WAN client's choice | library defaults; a level knob                  |
| One compressor per response, zstd at concurrency one             | no package-level state; no goroutines per response                                         | a `sync.Pool` of writers, until the bench asks  |
| zstd lands with gzip                                             | already vendored and linked; the brief's open question is answered by the tree             | zstd in M4                                      |
| The writer stays unexported                                      | M3's legacy wart (substring `gzip`, every route) exports what it needs when it exists      | a matcher parameter now                         |
