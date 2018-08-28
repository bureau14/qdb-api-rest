#!/bin/bash
if [ "$#" -ne 3 ]; then
    echo "Usage: package /path/to/qdb-api-rest-server/binary /path/to/swagger.json os_name"
    exit
fi

EXE_PATH=$1;shift
SWAGGER_PATH=$1;shift
OS=$1;shift

VERSION=`cat $SWAGGER_PATH | grep "\"version\":" | awk -F '"' '{print $4}'`

mkdir bin
mv $EXE_PATH bin/qdb-api-rest-server
mkdir -p share/qdb
mv default.cfg share/qdb/default.cfg

tar cvzf qdb-$VERSION-$OS-api-rest.tar.gz bin share