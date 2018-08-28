#!/usr/bin/env sh

QDB_REST_SERVER=`which qdb-api-rest-server`

if [ "$#" -ne 1 ]; then
    echo "Usage: qdb-api-rest-server cluster_uri"
    exit
fi
CLUSTER_URI=$1; shift

sed -i -e 's|"cluster_uri": *"[^"]*",|"cluster_uri": "'"${CLUSTER_URI}"'",|' /var/lib/qdb/qdb-api-rest.cfg

${QDB_REST_SERVER} --config-file /var/lib/qdb/qdb-api-rest.cfg
