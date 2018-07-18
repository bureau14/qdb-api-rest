# qdb-api-rest

## I. Build On Docker
```
docker build -t rest-api-server .
```

## II. Run On Docker
### Setup quasardb
```
docker pull bureau14/qdb
```

#### Unique node
```
docker run -d --name qdb-server bureau14/qdb --security=false
```

#### Multiple nodes
```
docker run -d --name qdb-server1 bureau14/qdb -v /db1:/var/lib/qdb --security=false --log-level=debug
docker run -d --name qdb-server2 --link qdb-server1:successor bureau14/qdb -v /db2:/var/lib/qdb --peer successor:2836 --security=false --log-level=debug
docker run -d --name qdb-server3 --link qdb-server2:successor bureau14/qdb -v /db3:/var/lib/qdb --peer successor:2836 --security=false --log-level=debug
```

### Run
```
docker run -it --link qdb-server:qdb -p 40000:40000 rest-api-server qdb://qdb:2836
```

## III. Build locally
```
go get -u github.com/go-swagger/go-swagger/cmd/swagger
$GOPATH/swagger generate server -f ./swagger.json -A qdb-rest-api
cp configure_qdb_rest.go restapi/configure_qdb_rest.go
```
