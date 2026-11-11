#!/usr/bin/env bash
# This is the Buildkite package step for qdb-api-rest.
# .buildkite/steps/_build.yml invokes it last, so the step publishes only
# a binary that passed the Go tests and the e2e round trip. It packs the
# binary that 20.build.sh built into
#
#     artifacts/qdb-<version>-<os>-<arch>-rest.tar.gz   (.zip on Windows)
#
# and the qdb-artifacts plugin uploads that file. qdb-pkg-archives
# republishes the archive as it is, and qdb-pkg-debian, qdb-pkg-rpm and
# qdb-pkg-windows build their packages from it.
#
# The archive holds two directories:
#   bin/  qdb_rest, and next to it the shared libraries the binary loads
#         on the platforms that link them dynamically
#   etc/  qdb_rest.yaml, the reference configuration

set -euxo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(dirname "$(dirname "${SCRIPT_DIR}")")"

source "${SCRIPT_DIR}/00.common.sh"

cd "${BASE_DIR}"

# The archive name keeps the <os>-<arch> token of the go-swagger server's
# archive, because qdb-docker and the download site address the file by it.
case "$(uname)" in
    Linux )   OS="linux" ;;
    Darwin )  OS="darwin" ;;
    FreeBSD ) OS="freebsd" ;;
    MINGW* )  OS="windows" ;;
    * )
        echo "package: unsupported OS '$(uname)'" >&2
        exit 1
        ;;
esac

case "$(uname -m)" in
    x86_64 | amd64 )  ARCH="amd64" ;;
    aarch64 | arm64 ) ARCH="aarch64" ;;
    * )
        echo "package: unsupported architecture '$(uname -m)'" >&2
        exit 1
        ;;
esac

SUFFIX=""
if [[ "${OS}" == "windows" ]]; then
    SUFFIX=".exe"
fi

# Linux links the C API and DuckDB statically, and FreeBSD links DuckDB
# statically (.envrc). Every other library is loaded at run time, from the
# binary's own directory: the rpath has $ORIGIN on FreeBSD and
# @loader_path on macOS, and Windows searches the directory of the
# executable first.
case "${OS}" in
    linux )   RUNTIME_LIBS=() ;;
    freebsd ) RUNTIME_LIBS=("${BASE_DIR}/qdb/lib/libqdb_api.so") ;;
    darwin )  RUNTIME_LIBS=("${BASE_DIR}/qdb/lib/libqdb_api.dylib" "${BASE_DIR}/duck/lib/libduckdb.dylib") ;;
    windows ) RUNTIME_LIBS=("${BASE_DIR}/qdb/bin/qdb_api.dll" "${BASE_DIR}/duck/bin/duckdb.dll") ;;
esac

ARTIFACTS_DIR="${BASE_DIR}/artifacts"
PKG_DIR="${ARTIFACTS_DIR}/pkg"
rm -rf "${ARTIFACTS_DIR}"
mkdir -p "${PKG_DIR}/bin" "${PKG_DIR}/etc"

cp "${BASE_DIR}/bin/qdb_rest${SUFFIX}" "${PKG_DIR}/bin/"
# The ${a[@]+...} form is for bash 3.2 on macOS, where expanding an empty
# array under set -u is an error.
for lib in ${RUNTIME_LIBS[@]+"${RUNTIME_LIBS[@]}"}; do
    cp "${lib}" "${PKG_DIR}/bin/"
done

# The packages install this file as the configuration of the service, so
# no packaging repository keeps a copy of its own.
cp "${BASE_DIR}/examples/qdb_rest.yaml" "${PKG_DIR}/etc/"

# qdb-duck ships its library readable by its owner only, and the user the
# service runs as must be able to load it.
chmod 755 "${PKG_DIR}/bin"/*
chmod 644 "${PKG_DIR}/etc"/*

ARCHIVE_BASENAME="qdb-$(cat VERSION)-${OS}-${ARCH}-rest"
if [[ "${OS}" == "windows" ]]; then
    ARCHIVE="${ARTIFACTS_DIR}/${ARCHIVE_BASENAME}.zip"
    (cd "${PKG_DIR}" && 7z a -tzip "${ARCHIVE}" bin etc)
else
    ARCHIVE="${ARTIFACTS_DIR}/${ARCHIVE_BASENAME}.tar.gz"
    tar -czf "${ARCHIVE}" -C "${PKG_DIR}" bin etc
fi

# Only the archive stays in artifacts/, which the plugin uploads whole.
rm -rf "${PKG_DIR}"

echo "package: ${ARCHIVE}"
ls -l "${ARCHIVE}"
