#!/bin/bash

set -xe

function login {
    curl -k -H "Content-Type: application/json" -X POST --data-binary @empty.private http://127.0.0.1:40080/api/login | grep -o '"token": *"[^"]*"' | grep -o ': *"[^"]*"' | grep -o '"[^"]*"' | tr -d '"'
}


TOKEN=$(login)


curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"CREATE TABLE ts(col int64)"}' http://127.0.0.1:40080/api/query
curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"INSERT INTO ts($timestamp, col) VALUES(2017-01-01, 1)"}' http://127.0.0.1:40080/api/query
curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"INSERT INTO ts($timestamp, col) VALUES(2017-01-02, 2)"}' http://127.0.0.1:40080/api/query


EXPECTED='{"tables":[{"columns":[{"data":[2],"name":"count(col)","type":"count"}]}]}'
ERROR=0

# Without encoding
RESULT=`curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"select count(*) from ts in range (2017,+1y)"}' http://127.0.0.1:40080/api/query`
if [ $EXPECTED != $RESULT ]; then
    echo "Result does not match without encoding."
    ERROR=1
fi

# With encoding
RESULT=`curl -k -H "Accept-Encoding: gzip" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"select count(*) from ts in range (2017,+1y)"}' http://127.0.0.1:40080/api/query | gunzip`
if [ $EXPECTED != $RESULT ]; then
    echo "Result does not match with encoding."
    ERROR=1
fi

curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '{"query":"DROP TABLE ts"}' http://127.0.0.1:40080/api/query

curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '4' http://127.0.0.1:40080/api/option/parallelism

RESULT=`curl -k -H "Accept-Encoding: gzip" -H "Authorization: Bearer ${TOKEN}" -sb -X GET http://127.0.0.1:40080/api/option/parallelism | gunzip`
if [ "4" != $RESULT ]; then
    echo "Result does not match with encoding."
    ERROR=1
fi

curl -k -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -sb -X POST -d '8' http://127.0.0.1:40080/api/option/parallelism

RESULT=`curl -k -H "Accept-Encoding: gzip" -H "Authorization: Bearer ${TOKEN}" -sb -X GET http://127.0.0.1:40080/api/option/parallelism | gunzip`
if [ "8" != $RESULT ]; then
    echo "Result does not match with encoding."
    ERROR=1
fi

exit $ERROR