package meta

import "fmt"

const Version string = "3.15.0-nightly.0"
const GitHash string = "4df0625753830e88eb06d82991cbaf0d309669cb"
const BuildTime string = "2025-05-06 23:41:40 -0400"
const GoVersion string = "go1.24.2"
const BuildType string = "Debug"
const Platform string = "linux-amd64"

const versionInfoTemplate string = `
quasardb rest api version: %s
build: %s
date: %s

compiler: %s
 
build type: %s
platform: %s

Copyright (c) 2009-2025, quasardb SAS. All rights reserved.
`

func GetVersionInfoString() string {
	return fmt.Sprintf(versionInfoTemplate, Version, GitHash, BuildTime, GoVersion, BuildType, Platform)
}
