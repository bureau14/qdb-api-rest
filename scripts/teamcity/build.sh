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
env GOOS=linux GOARCH=arm go build -x -v -o qdb_rest$SUFFIX

# Build qdb_rest_service on windows
case $(uname) in
    MINGW* )
        cd ../qdb_rest_service
        go build -x -v -o qdb_rest_service$SUFFIX
        ;;
esac