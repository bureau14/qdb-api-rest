# qdb-api-rest

## I. Build On Docker
```
docker build -t qdb-api-rest-server .
```

## II. Run On Docker
### Setup quasardb
```
docker pull bureau14/qdb:nightly
```

#### Unique node
```
docker run -d --name qdb-server bureau14/qdb:nightly --security=false
```

#### Multiple nodes
```
docker run -d --name qdb-server1 bureau14/qdb:nightly -v /db1:/var/lib/qdb --security=false --log-level=debug
docker run -d --name qdb-server2 --link qdb-server1:successor bureau14/qdb:nightly -v /db2:/var/lib/qdb --peer successor:2836 --security=false --log-level=debug
docker run -d --name qdb-server3 --link qdb-server2:successor bureau14/qdb:nightly -v /db3:/var/lib/qdb --peer successor:2836 --security=false --log-level=debug
```

#### Run
Make the rest-api config file accessible in docker then run
```
export QDB_REST_DIR=`pwd`/qdb-var-rest
mkdir $QDB_REST_DIR
cp rest-api.cfg $QDB_REST_DIR/.
docker run -it --link qdb-server:qdb -v $QDB_REST_DIR:/var/lib/qdb -p 40000:40000 qdb-api-rest-server qdb://qdb:2836
```

### Running with security on
Install qdb-server on your own machine to make qdb_user_add and qdb_cluster_keygen available.
```
wget https://download.quasardb.net/quasardb/nightly/server/qdb-3.0.0master-linux-64bit-server.tar.gz
tar qdb-3.0.0master-linux-64bit-server.tar.gz --no-same-owner -C /usr/local/
rm qdb-3.0.0master-linux-64bit-server.tar.gz
```

##### Generate the different keys
```
qdb_user_add -u rest-api -s rest-api.private -p users.cfg
qdb_user_add -u tintin -s tintin.private -p users.cfg
```
```
qdb_cluster_keygen -p cluster.public -s cluster.private
```
##### Move those keys and config to the appropriate directories
```
export QDB_REST_DIR=`pwd`/qdb-var-rest
mkdir $QDB_REST_DIR
cp rest-api.private $QDB_REST_DIR/.
cp cluster.public $QDB_REST_DIR/.
cp rest-api.cfg $QDB_REST_DIR/.
```
```
export QDB_QDDB_DIR=`pwd`/qdb-var-qdbd
mkdir $QDB_QDDB_DIR
cp users.cfg $QDB_QDDB_DIR/.
cp cluster.private $QDB_QDDB_DIR/.
```

##### Run the server
```
sudo docker run -d -v $QDB_QDDB_DIR:/var/lib/qdb --name qdb-server bureau14/qdb:nightly --cluster-private-file /var/lib/qdb/cluster.private --user-list /var/lib/qdb/users.cfg
```
##### Run the rest API
```
sudo docker run -it --link qdb-server:qdb -p 40000:40000 -v $QDB_REST_DIR:/var/lib/qdb qdb-api-rest-server qdb://qdb:2836
```


## III. Build locally
```
go get -u github.com/go-swagger/go-swagger/cmd/swagger
$GOPATH/swagger generate server -f ./swagger.json -A qdb-api-rest -P models.Principal
cp configure_qdb_rest.go restapi/configure_qdb_rest.go
go install ./...
```

## IV. Example
#### Login
```
curl -k -H 'Origin: http://0.0.0.0:3449'  -H "Content-Type: application/json" -X POST --data-binary @empty.private https://127.0.0.1:40000/api/login
```
#### Get cluster information
```
curl -i http://127.0.0.1:40000/api/cluster
```
#### Get node information
```
curl -i http://127.0.0.1:40000/api/cluster/nodes/127.0.0.1:2836
```
#### Run a query
```
curl -sb -X POST -H "Content-Type: application/json" -d '"select count(*) from timeseries in range (2017,+1y)"' http://127.0.0.1:40000/api/query
```

## V. Config File
The config file need to specify the following values:
1. allowed_origins (http://localhost:3449,https://localhost:3449,http://0.0.0.0:3449,https://0.0.0.0:3449)
1. cluster_public_key_file (/var/lib/qdb/cluster.public)
1. rest_private_key_file (/var/lib/qdb/rest-api.private)
1. assets (/usr/share/qdb/assets)