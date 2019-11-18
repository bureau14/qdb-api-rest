#!/bin/bash

if [ "$#" -ne 1 ]; then
    echo "Usage: get_dependencies /path/to/qdb-api-rest"
    exit
fi

cd $1
# this permits to create the home folder of gopath
# it also serves as a fallback in case we don't find any version
# TODO(vianney): find a better solution to the vendor problem
go get github.com/go-swagger/go-swagger/cmd/swagger
case $(uname) in
    MINGW* )
        curl -sL "https://github.com/go-swagger/go-swagger/releases/download/v0.17.2/swagger_windows_amd64.exe" > swagger-0.17.2.exe
        SWAGGER="./swagger-0.17.2.exe"
        ;;
    Darwin )
        curl -sL "https://github.com/go-swagger/go-swagger/releases/download/v0.17.2/swagger_darwin_amd64" > swagger-0.17.2
        chmod +x swagger-0.17.2
        SWAGGER="./swagger-0.17.2"
        ;;
    Linux )
        curl -sL "https://github.com/go-swagger/go-swagger/releases/download/v0.17.2/swagger_linux_amd64" > swagger-0.17.2
        chmod +x swagger-0.17.2
        SWAGGER="./swagger-0.17.2"
        ;;
    * )
        # fallback
        SWAGGER="swagger"
        # echo "##teamcity[buildProblem description='Unknown platform: uname returned $(uname)' identity='unknown-platform']"
        ;;
esac

$SWAGGER generate server -f ./swagger.json -A qdb-api-rest -P models.Principal --exclude-main

go get -d ./...
go get -d "github.com/bureau14/qdb-api-go"