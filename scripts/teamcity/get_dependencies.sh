#!/bin/bash

if [ "$#" -ne 1 ]; then
    echo "Usage: get_dependencies /path/to/qdb-api-rest"
    exit
fi

go get github.com/go-swagger/go-swagger/cmd/swagger
swagger generate server -f ./swagger.json -A qdb-api-rest -P models.Principal --exclude-main

cd $1
mv configure_qdb_api_rest.go restapi/configure_qdb_api_rest.go
go get -d ./...