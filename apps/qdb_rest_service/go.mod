module github.com/bureau14/qdb-api-rest/apps/qdb_rest_service

go 1.17

replace github.com/bureau14/qdb-api-go/v3 => ../../../qdb-api-go

replace github.com/bureau14/qdb-api-rest => ../../

replace github.com/bureau14/qdb-api-rest/prometheus => ../../prometheus

replace github.com/bureau14/qdb-api-rest/restapi => ../../restapi

replace github.com/bureau14/qdb-api-rest/restapi/operations => ../../restapi/operations

replace github.com/bureau14/qdb-api-rest/qdbinterface => ../../qdbinterface
