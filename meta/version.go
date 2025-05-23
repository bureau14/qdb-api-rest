package meta

import "fmt"

const Version string = "3.15.0-nightly.0"
const GitHash string = ""
const BuildTime string = ""
const GoVersion string = ""
const Platform string = ""

const versionInfoTemplate string = `
quasardb rest api version: %s
build: %s
date: %s

compiler: %s

platform: %s

Copyright (c) 2009-2025, quasardb SAS. All rights reserved.
`

func GetVersionInfoString() string {
	return fmt.Sprintf(versionInfoTemplate, Version, GitHash, BuildTime, GoVersion, Platform)
}
