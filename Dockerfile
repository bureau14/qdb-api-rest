FROM golang:1.9


# the quasardb c API is required for qdb-api-go
# only the nightly version supports `qdb_cluster_endpoints`
RUN wget https://download.quasardb.net/quasardb/nightly/api/c/qdb-3.0.0master-linux-64bit-c-api.tar.gz
RUN tar xzf qdb-3.0.0master-linux-64bit-c-api.tar.gz -C /usr
RUN ldconfig
RUN rm qdb-3.0.0master-linux-64bit-c-api.tar.gz

# Generate self-signed certificate
RUN mkdir /etc/qdb
RUN openssl req -newkey rsa:4096 -nodes -sha512 -x509 -days 3650 -nodes -out /etc/qdb/rest-api.cert.pem -keyout /etc/qdb/rest-api.key.pem -subj "/C=FR/L=Paris/O=Quasardb/CN=Quasardb"

RUN mkdir -p /go/src/github.com/bureau14/qdb-api-rest

WORKDIR /go/src/github.com/bureau14/qdb-api-rest
COPY configure_qdb_api_rest.go .
COPY swagger.json .
ADD qdbinterface/ qdbinterface
ADD .git/ .git
ADD cmd/ cmd 

RUN go get -u github.com/go-swagger/go-swagger/cmd/swagger
RUN swagger generate server -f ./swagger.json -A qdb-api-rest -P models.Principal --exclude-main
RUN cp configure_qdb_api_rest.go restapi/configure_qdb_api_rest.go

RUN go get -d -v ./...
RUN go install -v ./...

EXPOSE 40080

ADD qdb-api-rest-wrapper.sh /usr/bin/
RUN chmod +x /usr/bin/qdb-api-rest-wrapper.sh
ENTRYPOINT ["/usr/bin/qdb-api-rest-wrapper.sh"]
