package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// tablesPath is the collection; a table is tablesPath + "/" + its name.
const tablesPath = "/api/v2/tables"

// maxShardSize is the largest shard_size a time.Duration can carry.
const maxShardSize = math.MaxInt64 / int64(time.Millisecond)

// columnRequest is one column of the create body, in the schema
// vocabulary; symtable belongs to a symbol column only.
type columnRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Symtable string `json:"symtable"`
}

// createTableRequest is the create body. shard_size is in milliseconds,
// the C API's unit, and has no default: a pointer tells absent from zero.
type createTableRequest struct {
	Name      string          `json:"name"`
	ShardSize *int64          `json:"shard_size"`
	Columns   []columnRequest `json:"columns"`
}

// errNoShardSize and errShardSizeRange are the two shape errors of the
// body that JSON decoding does not find by itself.
var (
	errNoShardSize    = errors.New("shard_size is required, in milliseconds")
	errShardSizeRange = errors.New("shard_size is out of range")
)

// decodeCreateTable reads the body's shape and nothing more: names, sizes
// and the column vocabulary are judged by whoever consumes them.
func decodeCreateTable(body []byte) (createTableRequest, error) {
	var req createTableRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return req, err
	}
	switch {
	case req.ShardSize == nil:
		return req, errNoShardSize
	case *req.ShardSize > maxShardSize:
		return req, errShardSizeRange
	}
	return req, nil
}

// shard is the shard size as the cluster call takes it.
func (r createTableRequest) shard() time.Duration {
	return time.Duration(*r.ShardSize) * time.Millisecond
}

// columns are the body's columns as the cluster call takes them.
func (r createTableRequest) columns() []qdb.Column {
	cols := make([]qdb.Column, len(r.Columns))
	for i, c := range r.Columns {
		cols[i] = qdb.Column{Name: c.Name, Type: c.Type, Symtable: c.Symtable}
	}
	return cols
}

// caller is the cluster user the bearer middleware verified.
func caller(r *http.Request) qdb.User {
	c := auth.ClaimsFrom(r.Context())
	return qdb.User{Username: c.Username, SecretKey: c.SecretKey}
}

// handleCreateTable creates the table the body describes and answers 201
// with its Location. A taken name is 409; an invalid column and anything
// else the cluster answered are the caller's 400.
func handleCreateTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// A non-JSON body is a clear 415 instead of a decode error.
	if !isJSON(r.Header.Get("Content-Type")) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	req, err := decodeCreateTable(body)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	err = qdb.ClusterFrom(ctx).CreateTable(ctx, caller(r), req.Name, req.shard(), req.columns())
	switch {
	case err == nil:
		w.Header().Set("Location", tablesPath+"/"+url.PathEscape(req.Name))
		w.WriteHeader(http.StatusCreated)
	case qdb.IsTableExists(err):
		writeProblem(w, http.StatusConflict, err.Error())
	default:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// handleDeleteTable removes the table the path names and answers 204; a
// name the cluster does not know is 404.
func handleDeleteTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := qdb.ClusterFrom(ctx).RemoveTable(ctx, caller(r), r.PathValue("name"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case qdb.IsTableNotFound(err):
		writeProblem(w, http.StatusNotFound, err.Error())
	default:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// registerTableRoutes serves the table collection behind the bearer
// middleware. The mux patterns fix method and path.
func registerTableRoutes(mux *http.ServeMux) {
	mux.Handle("POST "+tablesPath, withCompression(requireBearer(http.HandlerFunc(handleCreateTable))))
	mux.Handle("DELETE "+tablesPath+"/{name}", withCompression(requireBearer(http.HandlerFunc(handleDeleteTable))))
}
