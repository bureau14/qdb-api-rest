FROM golang:1.9

RUN mkdir -p /go/src/github.com/bureau14/qdb-api-rest

WORKDIR /go/src/github.com/bureau14/qdb-api-rest
COPY . .

RUN go get -u github.com/go-swagger/go-swagger/cmd/swagger
RUN swagger generate server -f ./swagger.json -A qdb-api-rest
RUN cp configure_qdb_api_rest.go restapi/configure_qdb_api_rest.go

RUN go get -d -v ./...

# the quasardb c API is required for qdb-api-go
# only the nightly version supports `qdb_cluster_endpoints`
RUN wget https://download.quasardb.net/quasardb/nightly/api/c/qdb-2.8.0master-linux-64bit-c-api.tar.gz
RUN tar xzf qdb-2.8.0master-linux-64bit-c-api.tar.gz -C /usr
RUN ldconfig
RUN rm qdb-2.8.0master-linux-64bit-c-api.tar.gz

RUN go install -v ./...

EXPOSE 40000

ADD qdb-api-rest-wrapper.sh /usr/bin/
RUN chmod +x /usr/bin/qdb-api-rest-wrapper.sh
ENTRYPOINT ["/usr/bin/qdb-api-rest-wrapper.sh"]
