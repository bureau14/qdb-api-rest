#!/bin/bash
if [ "$#" -ne 1 ]; then
    echo "Usage: build /path/to/qdb-api-rest"
    exit
fi

case $(uname) in
    MINGW* )
        SUFFIX=.exe
        ;;
esac

# Build qdb_rest
cd $1/apps/qdb_rest
go build -x -v -gcflags=-trimpath=$HOME/go -o qdb_rest$SUFFIX

# Build qdb_rest_service on windows
case $(uname) in
    MINGW* )
        cd ../qdb_rest_service
        go build -x -v -gcflags=-trimpath=c:\Go -o qdb_rest_service$SUFFIX
        ;;
esac