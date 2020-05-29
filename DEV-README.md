# Update schema
go get github.com/go-swagger/go-swagger/cmd/swagger
~/go/bin/swagger generate server -f ./swagger.json -A qdb-api-rest -P models.Principal --exclude-main