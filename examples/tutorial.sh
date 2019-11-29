#!/usr/bin/env bash

USERNAME=anonymous
SECRET=

LOGIN_RESPONSE=$(
# connect-start
curl -X POST \
  -H "Content-Type: application/json" \
  -d '{"username": "$USERNAME", "secret_key": "$SECRET"}' \
  http://127.0.0.1:40080/api/login
# connect-end
)

TOKEN=$( echo $LOGIN_RESPONSE | jq -r ".token" )

# query-start
curl \
  -X POST \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{ "query": "SELECT SUM(volume) FROM stocks" }' \
  http://127.0.0.1:40080/api/query
# query-end