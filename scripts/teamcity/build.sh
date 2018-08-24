#!/bin/bash
if [ "$#" -ne 1 ]; then
    echo "Usage: build /path/to/cmd/qdb-api-rest-server"
    exit
fi
cd $1
go build -x -v -o qdb-api-rest-server