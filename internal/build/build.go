// Package build holds the build metadata the linker sets through
// -ldflags -X: the version, the commit, the build time, the build mode
// and the GOAMD64 level. Nothing writes these at runtime, so they are
// the binary's constants, read by main for --version and by the session
// handler for the version. The root Makefile and scripts/cicd/20.build.sh
// compose the -X list, under the rule of scripts/cicd/AGENTS.md, and the
// VERSION file at the repository root is the one version-string
// location (docs/brief.md, Versioning and release), so no version
// constant lives in source.
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
