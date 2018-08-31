#!/bin/bash
if [ "$#" -ne 3 ]; then
    echo "Usage: package /path/to/qdb-api-rest-server/binary /path/to/swagger.json os_name"
    exit
fi

EXE_PATH=$1;shift
SWAGGER_PATH=$1;shift
OS_NAME=$1;shift

VERSION=`cat $SWAGGER_PATH | grep "\"version\":" | awk -F '"' '{print $4}'`

mkdir bin
mv $EXE_PATH bin/
mkdir -p share/qdb
mv default.cfg share/qdb/default.cfg

case $(uname) in
    MINGW* )
        ZIP="7z a -y"
        SUFFIX=".zip"
        ;;
    * )
        ZIP="tar cvzf"
        SUFFIX=".tar.gz"
        ;;
esac

$ZIP qdb-$VERSION-$OS_NAME-api-rest$SUFFIX bin share
