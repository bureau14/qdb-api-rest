// Package qdb wraps qdb-api-go for the rest of the server. APIVersion and
// APIBuild expose the identity of the linked libqdb_api -- static
// libqdb_api.a on Linux, the shared library elsewhere -- without opening
// a handle, which is what proves the cgo link on every platform.
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
