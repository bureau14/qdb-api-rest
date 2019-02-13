#!/bin/sh
set -eu -o pipefail
IFS=$'\n\t'

if [[ $# -ne 1 ]] ; then
    >&2 echo "Usage: $0 <new_version>"
    exit 1
fi

INPUT_VERSION=$1; shift

MAJOR_VERSION=${INPUT_VERSION%%.*}
WITHOUT_MAJOR_VERSION=${INPUT_VERSION#${MAJOR_VERSION}.}
MINOR_VERSION=${WITHOUT_MAJOR_VERSION%%.*}
WITHOUT_MINOR_VERSION=${INPUT_VERSION#${MAJOR_VERSION}.${MINOR_VERSION}.}
PATCH_VERSION=${WITHOUT_MINOR_VERSION%%.*}

XYZ_VERSION="${MAJOR_VERSION}.${MINOR_VERSION}.${PATCH_VERSION}"

if [[ "${INPUT_VERSION}" == *-* ]] ; then
    TAGS_VERSION=${INPUT_VERSION#*-}
    TAGS_VERSION=${TAGS_VERSION/nightly.*/}
    # TODO(marek): Actually, we should put here the branch name instead of "master".
    FULL_XYZ_VERSION="${XYZ_VERSION}master"
else
    FULL_XYZ_VERSION="${XYZ_VERSION%%-*}"
fi

cd $(dirname -- $0)
cd ${PWD}/../..

sed -i -e 's/"version": *"[^"]*",/"version": "'"${FULL_XYZ_VERSION}"'",/' swagger.json
sed -i -e 's/const version string = .*/const version string = "'${FULL_XYZ_VERSION}'"/' configure_qdb_api_rest.go
