# qdb-api-rest

## I. Try it with docker
### Preliminary
##### Download quasardb tools
Install qdb-server on your own machine to make qdb_user_add and qdb_cluster_keygen available.
```
wget https://download.quasardb.net/quasardb/nightly/server/qdb-3.0.0master-linux-64bit-server.tar.gz
tar qdb-3.0.0master-linux-64bit-server.tar.gz --no-same-owner -C /usr/local/
rm qdb-3.0.0master-linux-64bit-server.tar.gz
```
##### Generate a user key
```
/usr/local/bin/qdb_user_add -u tintin -s tintin.private -p users.cfg
```
##### Generate a cluster key
```
/usr/local/bin/qdb_cluster_keygen -p cluster.public -s cluster.private
```

#### Setup quasardb
```
docker pull bureau14/qdb:nightly

export QDB_QDDB_DIR=`pwd`/qdb-var-qdbd
mkdir $QDB_QDDB_DIR
cp users.cfg $QDB_QDDB_DIR/.
cp cluster.private $QDB_QDDB_DIR/.
```

### Build
```
docker build -t qdb-api-rest-server .
```

### Setup
```
export QDB_REST_DIR=`pwd`/qdb-var-rest
mkdir $QDB_REST_DIR
cp cluster.public $QDB_REST_DIR/.
cp rest-api.cfg $QDB_REST_DIR/.
```

### Run
```
docker run -d -v $QDB_QDDB_DIR:/var/lib/qdb --name qdb-server bureau14/qdb:nightly --cluster-private-file /var/lib/qdb/cluster.private --user-list /var/lib/qdb/users.cfg

docker run -it --link qdb-server:qdb -v $QDB_REST_DIR:/var/lib/qdb -p 40080:40080 qdb-api-rest-server qdb://qdb:2836
```

## II. Build locally
```
go get -u github.com/go-swagger/go-swagger/cmd/swagger
$GOPATH/swagger generate server -f ./swagger.json -A qdb-api-rest -P models.Principal --exclude-main
cp configure_qdb_rest.go restapi/configure_qdb_rest.go
go install ./...
```

## IV. Example
#### Login
```
curl -k -H 'Origin: http://0.0.0.0:3449'  -H "Content-Type: application/json" -X POST --data-binary @tintin.private https://127.0.0.1:40080/api/login
```
#### Get cluster information
```
curl -k -H "Authorization: Bearer ${TOKEN}" -H 'Origin: http://0.0.0.0:3449' -i https://localhost:40080/api/cluster
```
#### Get node information
```
curl -k -H "Authorization: Bearer ${TOKEN}" -H 'Origin: http://0.0.0.0:3449' -i http://127.0.0.1:40080/api/cluster/nodes/127.0.0.1:2836
```
#### Run a query
```
curl -k -H "Authorization: Bearer ${TOKEN}" -H 'Origin: http://0.0.0.0:3449' -sb -X POST -H "Content-Type: application/json" -d '"select count(*) from timeseries in range (2017,+1y)"' http://127.0.0.1:40080/api/query
```

## V. Config File
The config file need to specify the following values:
1. allowed_origins         - description: "allowed origins"
1. cluster_uri             - description: "the uri of the cluster"
1. cluster_public_key_file - description: "cluster public key path"
1. tls_certificate         - description: "certificate path"
1. tls_key                 - description: "certificate key path"
1. host                    - description: "host of the rest api"
1. port                    - description: "port of the rest api"
1. assets                  - description: "served assets path"
