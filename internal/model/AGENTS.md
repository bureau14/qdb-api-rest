# internal/model -- Agent Instructions

Scope: the neutral table representation. Package-wide Go rules:
`internal/AGENTS.md`.

- `model` holds the types the layers hand each other and that no one of
  them owns. A type belongs here when at least two packages share it and
  it describes data. The package is Arrow by indirection. The record
  batch is the representation, and a consumer that needs the shape of a
  table takes a `model` type and never an `internal/qdb` or
  `internal/encoding` one.
- The package does no I/O, logs nothing, and knows no wire format and no
  C API. It imports `arrow` and the standard library only, so both
  `internal/encoding` and `internal/qdb` import it and it imports
  neither. A type that needs the binding stays in `internal/qdb`, and a
  type that needs a format stays in `internal/encoding`.
- The type states its ownership: who releases a batch, and when.
