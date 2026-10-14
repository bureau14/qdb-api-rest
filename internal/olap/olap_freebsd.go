//go:build freebsd

// Package olap is a stub on FreeBSD, where the binary carries no DuckDB,
// because qdb-duck does not build there (ADR-0017). It keeps the package's
// surface so every caller compiles on every platform.
package olap

// Version is empty on FreeBSD, because no DuckDB library is linked.
func Version() string {
	return ""
}
