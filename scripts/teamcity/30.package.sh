#!/usr/bin/env bash

set -eux

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/../common.sh"


SWAGGER_PATH=${BASE_DIR}/swagger.json
QDB_REST_BINARY=${QDB_REST_DIR}/qdb_rest

case $(uname) in
    MINGW* )
        QDB_REST_BINARY=$QDB_REST_BINARY.exe
        ;;
esac

VERSION=`cat $SWAGGER_PATH | grep "\"version\":" | awk -F '"' '{print $4}'`

mkdir bin
mkdir etc

cp $QDB_REST_BINARY bin/

case $(uname) in
    MINGW* )
        ZIP="7z a -y"
        SUFFIX=".zip"

        # Include qdb_rest_service
        QDB_REST_SERVICE_BINARY=${QDB_REST_SERVICE_DIR}/qdb_rest_service.exe
        mv ${QDB_REST_SERVICE_BINARY} bin/

        # Include openssl
        cp -v ${BASE_DIR}/scripts/teamcity/openssl.cnf etc/openssl.conf

        curl -s https://teamcity-agentbuilddeps-20241223095405875100000001.s3.eu-west-1.amazonaws.com/windows/openssl/openssl-1.0.2q-x64_86-win64.zip > openssl.zip
        7z x openssl.zip
        mv openssl.exe bin/
        mv libeay32.dll bin/
        mv ssleay32.dll bin/
        mv "OpenSSL License.txt" etc/
        rm openssl.zip

        cp -v ${BASE_DIR}/qdb_rest.windows.conf.sample etc/qdb_rest.conf.sample
        ;;
    * )
        ZIP="tar cvzf"
        SUFFIX=".tar.gz"

        cp -v ${BASE_DIR}/qdb_rest.unix.conf.sample etc/qdb_rest.conf.sample
        ;;
esac

cp -v ${BASE_DIR}/qdb_rest.local.conf.sample etc/qdb_rest.local.conf.sample

$ZIP qdb-${VERSION}-${PLATFORM}-rest${SUFFIX} bin etc
