module github.com/bureau14/qdb-api-rest/apps/qdb_rest

go 1.17

replace github.com/bureau14/qdb-api-go/v3 => ../../../qdb-api-go

replace github.com/bureau14/qdb-api-rest => ../../

replace github.com/bureau14/qdb-api-rest/prometheus => ../../prometheus

replace github.com/bureau14/qdb-api-rest/restapi => ../../restapi

replace github.com/bureau14/qdb-api-rest/restapi/operations => ../../restapi/operations

replace github.com/bureau14/qdb-api-rest/qdbinterface => ../../qdbinterface

require (
	github.com/bureau14/qdb-api-rest/restapi v0.0.0-00010101000000-000000000000
	github.com/bureau14/qdb-api-rest/restapi/operations v0.0.0-00010101000000-000000000000
	github.com/go-openapi/loads v0.20.3
	github.com/jessevdk/go-flags v1.5.0
)

require (
	github.com/PuerkitoBio/purell v1.1.1 // indirect
	github.com/PuerkitoBio/urlesc v0.0.0-20170810143723-de5bf2ad4578 // indirect
	github.com/asaskevich/govalidator v0.0.0-20200907205600-7a23bdc65eef // indirect
	github.com/bureau14/qdb-api-go/v3 v3.10.0 // indirect
	github.com/bureau14/qdb-api-rest v0.0.0-00010101000000-000000000000 // indirect
	github.com/bureau14/qdb-api-rest/prometheus v0.0.0-00010101000000-000000000000 // indirect
	github.com/bureau14/qdb-api-rest/qdbinterface v0.0.0-00010101000000-000000000000 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/go-openapi/analysis v0.20.1 // indirect
	github.com/go-openapi/errors v0.20.1 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.19.6 // indirect
	github.com/go-openapi/runtime v0.20.0 // indirect
	github.com/go-openapi/spec v0.20.4 // indirect
	github.com/go-openapi/strfmt v0.20.3 // indirect
	github.com/go-openapi/swag v0.19.15 // indirect
	github.com/go-openapi/validate v0.20.3 // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.16.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/orcaman/concurrent-map v0.0.0-20210501183033-44dafcb38ecc // indirect
	github.com/prometheus/common v0.31.1 // indirect
	github.com/prometheus/prometheus v2.5.0+incompatible // indirect
	github.com/rs/cors v1.8.0 // indirect
	go.mongodb.org/mongo-driver v1.5.1 // indirect
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9 // indirect
	golang.org/x/net v0.0.0-20211013171255-e13a2654a71e // indirect
	golang.org/x/sys v0.0.0-20211013075003-97ac67df715c // indirect
	golang.org/x/text v0.3.7 // indirect
	google.golang.org/genproto v0.0.0-20200825200019-8632dd797987 // indirect
	google.golang.org/grpc v1.33.1 // indirect
	google.golang.org/protobuf v1.26.0-rc.1 // indirect
	gopkg.in/square/go-jose.v2 v2.6.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
