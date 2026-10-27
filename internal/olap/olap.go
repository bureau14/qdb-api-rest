// Package olap embeds DuckDB with the quasar extension, the engine behind
// full SQL over QuasarDB data. The library comes from qdb-duck's package
// in duck/, and the root .envrc locates it and selects the binding's
// build tags per OS (ADR-0017). The binding registers the static bundle's
// extensions from its package init, before any database opens, and the
// shared libraries register their own, so this package makes no
// registration call (ADR-0017, Alternatives). The package opens one
// instance per user and answers its queries as lent record batches
// (internal/olap/AGENTS.md).
package olap

import "github.com/duckdb/duckdb-go/v2/mapping"

// Version is the release of the DuckDB library the binary links, as the
// library reports it at run time, so it names what was linked rather
// than what the binding expected.
func Version() string {
	return mapping.LibraryVersion()
}
