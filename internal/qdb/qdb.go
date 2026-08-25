// Package qdb wraps qdb-api-go for the rest of the server: handle and
// session pools, the circuit breaker, query execution and ingestion land
// here. Today it exposes only the identity of the linked C API, which is
// what proves the cgo link -- static libqdb_api.a on Linux, the shared
// library elsewhere -- on every platform the binary is built for.
package qdb

import qdbapi "github.com/bureau14/qdb-api-go/v3"

// APIVersion returns the version string of the linked libqdb_api, read via
// qdb_version(); no handle is opened.
func APIVersion() string {
	return qdbapi.HandleType{}.APIVersion()
}

// APIBuild returns the build identifier of the linked libqdb_api, read via
// qdb_build(); no handle is opened.
func APIBuild() string {
	return qdbapi.HandleType{}.APIBuild()
}
