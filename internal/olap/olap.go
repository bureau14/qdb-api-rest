//go:build !freebsd

// Package olap embeds DuckDB with the quasar extension, the engine behind
// full SQL over QuasarDB data. The library comes from qdb-duck's package,
// located by the root .envrc: the static bundle on Linux and the dynamic
// library on macOS and Windows, which is why the binding's link tag
// differs per OS (duckdb_use_static_lib there, duckdb_use_lib here). The
// Arrow interface is enabled (duckdb_arrow), because a result leaves the
// engine as Arrow record batches. FreeBSD builds the stub next to this
// file, because qdb-duck does not build there (ADR-0017).
package olap

import "github.com/duckdb/duckdb-go/v2/mapping"

// Version is the release of the DuckDB library the binary links, as the
// library reports it at run time, so it names what was linked rather
// than what the binding expected.
func Version() string {
	return mapping.LibraryVersion()
}
