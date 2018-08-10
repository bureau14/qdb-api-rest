#!/usr/bin/env sh

QDB_REST_SERVER=`which qdb-api-rest-server`

if [ "$#" -ne 1 ]; then
    echo "Usage: qdb-api-rest-server cluster_uri"
    exit
fi
CLUSTER_URI=$1; shift

echo "cluster uri: ${CLUSTER_URI}"
${QDB_REST_SERVER} --tls-host 0.0.0.0 --tls-port 40000 --tls-certificate /etc/qdb/rest-api.cert.pem --tls-key /etc/qdb/rest-api.key.pem --config-file /var/lib/qdb/rest-api.cfg --cluster-uri ${CLUSTER_URI}
