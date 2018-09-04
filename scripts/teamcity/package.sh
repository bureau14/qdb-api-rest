#!/bin/bash
if [ "$#" -ne 2 ]; then
    echo "Usage: package /path/to/qdb-api-rest os_name"
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
mv qdb_rest.conf.sample etc/qdb_rest.conf.sample

case $(uname) in
    MINGW* )
        ZIP="7z a -y"
        SUFFIX=".zip"

        # Include qdb_rest_service
        QDB_REST_SERVICE_BINARY=$QDB_API_REST/apps/qdb_rest_service/qdb_rest_service.exe
        mv $QDB_REST_SERVICE_BINARY bin/

        # Include openssl
        curl -s https://indy.fulgan.com/SSL/openssl-1.0.2o-x64_86-win64.zip > openssl-1.0.2o-x64_86-win64.zip
        mv openssl.cnf > etc/openssl.conf
        7z x openssl-1.0.2o-x64_86-win64.zip
        mv openssl.exe bin/
        mv libeay32.dll bin/
        mv ssleay32.dll bin/
        mv "OpenSSL License.txt" etc/
        rm openssl-1.0.2o-x64_86-win64.zip
        ;;
    * )
        ZIP="tar cvzf"
        SUFFIX=".tar.gz"
        ;;
esac

$ZIP qdb-$VERSION-$OS_NAME-api-rest$SUFFIX bin etc
