#!/bin/sh
set -eu -o pipefail
IFS=$'\n\t'

if [[ $# -ne 1 ]] ; then
    >&2 echo "Usage: $0 <new_version>"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/../common.sh"

INPUT_VERSION=$1; shift

MAJOR_VERSION=${INPUT_VERSION%%.*}
WITHOUT_MAJOR_VERSION=${INPUT_VERSION#${MAJOR_VERSION}.}
MINOR_VERSION=${WITHOUT_MAJOR_VERSION%%.*}
WITHOUT_MINOR_VERSION=${INPUT_VERSION#${MAJOR_VERSION}.${MINOR_VERSION}.}
PATCH_VERSION=${WITHOUT_MINOR_VERSION%%.*}
XYZ_VERSION="${MAJOR_VERSION}.${MINOR_VERSION}.${PATCH_VERSION}"

SUB_RELEASE_VERSION=${INPUT_VERSION#*-}
SUB_RELEASE_TYPE=${SUB_RELEASE_VERSION%%.*}
SUB_RELEASE_MINOR_VERSION=${SUB_RELEASE_VERSION#*.}
if [[ "${SUB_RELEASE_TYPE}" == "rc" ]] ; then
    FULL_XYZ_VERSION="${XYZ_VERSION}-${SUB_RELEASE_TYPE}${SUB_RELEASE_MINOR_VERSION}"
else
    FULL_XYZ_VERSION="${XYZ_VERSION}"
fi

cd $(dirname -- $0)
cd ${PWD}/../..

sed -i -e 's/"version": *"[^"]*",/"version": "'"${FULL_XYZ_VERSION}"'",/' swagger.json

sed -i -e 's/const Version string = .*/const Version string = "'${FULL_XYZ_VERSION}'"/' meta/version.go
sed -i -e 's/const GitHash string = .*/const GitHash string = "'${GIT_HASH}'"/' meta/version.go
sed -i -e 's/const BuildTime string = .*/const BuildTime string = "'${CURRENT_DATETIME}'"/' meta/version.go
sed -i -e 's/const GoVersion string = .*/const GoVersion string = "'${GO_COMPILER_VERSION}'"/' meta/version.go
sed -i -e 's/const BuildType string = .*/const BuildType string = "'${BUILD_TYPE}'"/' meta/version.go
sed -i -e 's/const Platform string = .*/const Platform string = "'${PLATFORM}'"/' meta/version.go
