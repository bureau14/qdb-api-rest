#!/bin/bash

set -xe

if [[ -z ${QDB_DIR+set} ]]; then
    QDB_DIR="qdb/bin"
    echo "Setting QDB_DIR to ${QDB_DIR}"
fi

if [[ ! -d ${QDB_DIR} ]]; then
    echo "Please provide a valid binary directory, got: ${QDB_DIR}"
    exit 1
fi

set +u
QDB_REST="apps/qdb_rest/qdb_rest"
if [[ ${CMAKE_BUILD_TYPE} == "Debug" ]]; then
    QDB_REST="${QDB_REST}d"
fi

QDB_SHELL="${QDB_DIR}/qdbsh"
if [[ ${CMAKE_BUILD_TYPE} == "Debug" ]]; then
    QDB_SHELL="${QDB_SHELL}d"
fi

case "$(uname)" in
    MINGW*)
        cp ${QDB_DIR}/../bin/qdb_api.dll apps/qdb_rest/. || true
    ;;
    Darwin*)
        export DYLD_LIBRARY_PATH=$QDB_DIR/../lib
    ;;
    *)
        export LD_LIBRARY_PATH=$QDB_DIR/../lib
    ;;
esac

set -u

case "$(uname)" in
    MINGW*)
        QDB_REST=${QDB_REST}.exe
    ;;
    *)
    ;;
esac

FOUND=0

if [[ ! -f ${QDB_REST} ]]; then
    echo "Binary ${QDB_REST} not found."
    FOUND=1
fi

if [[ ! -f ${QDB_SHELL} ]]; then
    echo "Binary ${QDB_SHELL} not found."
    FOUND=1
fi

if [[ ${FOUND} != 0 ]] ; then
    echo "Binaries not found. Exiting..."
    exit 1
fi

QDB_REST_FILENAME=${QDB_REST##*/}