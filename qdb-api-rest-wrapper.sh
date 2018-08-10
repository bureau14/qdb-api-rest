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
export SERVER_PUBLIC_KEY_FILE=/var/lib/qdb/cluster.public
export REST_PRIVATE_KEY_FILE=/var/lib/qdb/rest-api.private

echo "cluster uri: ${QDB_REST_SERVER}"
echo "allowed origins: ${ALLOWED_ORIGINS}"
${QDB_REST_SERVER} --tls-host 0.0.0.0 --tls-port 40000 --tls-certificate /etc/qdb/rest-api.cert.pem --tls-key /etc/qdb/rest-api.key.pem
