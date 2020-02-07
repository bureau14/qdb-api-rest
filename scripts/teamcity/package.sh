#!/bin/bash
if [ "$#" -ne 2 ]; then
    echo "Usage: package /path/to/qdb-rest os_name"
    exit
fi

QDB_API_REST=$1; shift
OS_NAME=$1;shift

SWAGGER_PATH=$QDB_API_REST/swagger.json
QDB_REST_BINARY=$QDB_API_REST/apps/qdb_rest/qdb_rest

case $(uname) in
    MINGW* )
        QDB_REST_BINARY=$QDB_REST_BINARY.exe
        ;;
esac

VERSION=`cat $SWAGGER_PATH | grep "\"version\":" | awk -F '"' '{print $4}'`

mkdir bin
mv $QDB_REST_BINARY bin/
mkdir etc

case $(uname) in
    MINGW* )
        ZIP="7z a -y"
        SUFFIX=".zip"

        # Include qdb_rest_service
        QDB_REST_SERVICE_BINARY=$QDB_API_REST/apps/qdb_rest_service/qdb_rest_service.exe
        mv $QDB_REST_SERVICE_BINARY bin/

        # Include openssl
        curl -s https://qdbbuilddeps.s3.eu-central-1.amazonaws.com/windows/openssl/openssl-1.0.2q-x64_86-win64.zip > openssl.zip
        cp $QDB_API_REST/scripts/teamcity/openssl.cnf etc/openssl.conf
        7z x openssl.zip
        mv openssl.exe bin/
        mv libeay32.dll bin/
        mv ssleay32.dll bin/
        mv "OpenSSL License.txt" etc/
        rm openssl.zip

        mv qdb_rest.windows.conf.sample etc/qdb_rest.conf.sample
        ;;
    * )
        ZIP="tar cvzf"
        SUFFIX=".tar.gz"

        mv qdb_rest.unix.conf.sample etc/qdb_rest.conf.sample
        ;;
esac

mv qdb_rest.local.conf.sample etc/qdb_rest.local.conf.sample

$ZIP qdb-$VERSION-$OS_NAME-rest$SUFFIX bin etc
