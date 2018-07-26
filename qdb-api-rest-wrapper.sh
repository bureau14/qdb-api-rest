#!/usr/bin/env sh

QDB_REST_SERVER=`which qdb-api-rest-server`

if [ "$#" -ne 1 ]; then
    echo "Usage: qdb-api-rest-server cluster_uri"
    exit
fi
CLUSTER=$1; shift

export CLUSTER_URI=${CLUSTER}

echo ${QDB_REST_SERVER}
${QDB_REST_SERVER} --host 0.0.0.0 --port 40000
