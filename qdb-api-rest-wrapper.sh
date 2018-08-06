#!/usr/bin/env sh

QDB_REST_SERVER=`which qdb-api-rest-server`

if [ "$#" -ne 2 ]; then
    echo "Usage: qdb-api-rest-server cluster_uri allowed_origins"
    exit
fi
CLUSTER=$1; shift
ALLOWED=$1; shift

export CLUSTER_URI=${CLUSTER}
export ALLOWED_ORIGINS=${ALLOWED}

echo "cluster uri: ${QDB_REST_SERVER}"
echo "allowed origins: ${ALLOWED_ORIGINS}"
${QDB_REST_SERVER} --host 0.0.0.0 --port 40000
