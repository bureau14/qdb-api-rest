#!/bin/bash

set -xe

TOKEN=`curl -k -H "Content-Type: application/json" -X POST --data-binary @empty.private http://127.0.0.1:40080/api/login | grep -o '"token": *"[^"]*"' | grep -o ': *"[^"]*"' | grep -o '"[^"]*"' | tr -d '"'`

echo "token: $TOKEN"

curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"CREATE TABLE ts(col int64)"}' http://127.0.0.1:40080/api/query
curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"INSERT INTO ts($timestamp, col) VALUES(2017-01-01, 1)"}' http://127.0.0.1:40080/api/query
curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"INSERT INTO ts($timestamp, col) VALUES(2017-01-02, 2)"}' http://127.0.0.1:40080/api/query

EXPECTED='{"tables":[{"columns":[{"data":[2],"name":"count(col)","type":"count"}]}]}'
RESULT=`curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"select count(*) from ts in range (2017,+1y)"}' http://127.0.0.1:40080/api/query`

curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"DROP TABLE ts"}' http://127.0.0.1:40080/api/query

if [ $EXPECTED != $RESULT ]; then
    echo "Result does not match!"
    exit 1
fi
