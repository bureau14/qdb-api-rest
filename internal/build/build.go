// Package build holds the build metadata the linker sets through
// -ldflags -X: the version, the commit, the build time, the build mode
// and the GOAMD64 level. Nothing writes these at runtime, so they act as
// the binary's constants. main reads them for --version, and the session
// handler reads them for the version it answers. The root Makefile and
// scripts/cicd/20.build.sh compose the -X list under the rule of
// scripts/cicd/AGENTS.md. The version string lives in the VERSION file
// at the repository root (docs/brief.md, Versioning and release), so no
// version constant lives in source.
package build

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
	BuildMode = "unknown"
	// GOAMD64 stays empty on non-amd64 targets and on builds that do not
	// pin a microarchitecture level.
	GOAMD64 = ""
)
