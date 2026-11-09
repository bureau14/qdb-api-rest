package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// tablesPath is the collection. A table is tablesPath + "/" + its name.
const tablesPath = "/api/v2/tables"

// maxShardSize is the largest shard_size a time.Duration can carry.
const maxShardSize = math.MaxInt64 / int64(time.Millisecond)

// columnRequest is one column of the create body and of the table body,
// in the schema vocabulary. symtable belongs to a symbol column only.
type columnRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Symtable string `json:"symtable,omitempty"`
}

// createTableRequest is the create body. shard_size is in milliseconds,
// the C API's unit, and it has no default. A pointer tells an absent
// shard_size from a zero one.
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

// decodeCreateTable checks the body's shape only. The code that
// consumes the names, the sizes and the column types validates them.
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
// with its Location. A taken name is 409. An invalid column and anything
// else the cluster answered are the caller's 400.
func handleCreateTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// A non-JSON body gets a clear 415 instead of a decode error.
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

// handleDeleteTable removes the table the path names and answers 204. A
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

// tablesResponse is the list body. The names sit under a key, so a field
// can join them later without a break.
type tablesResponse struct {
	Tables []string `json:"tables"`
}

// withPrefix answers the names that start with the prefix, in their
// order, and an empty list when none does. The empty prefix keeps every
// name.
func withPrefix(names []string, prefix string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}

// handleListTables answers the table names as the caller sees them, under
// ?prefix= when given. Any cluster error is the caller's 400.
func handleListTables(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	names, err := qdb.ClusterFrom(ctx).Tables(ctx, caller(r))
	if err != nil {
		writeClusterError(ctx, w, err, http.StatusBadRequest)
		return
	}
	// The prefix is applied in Go, because show tables takes none.
	names = withPrefix(names, r.URL.Query().Get("prefix"))
	w.Header().Set("Content-Type", "application/json")
	// withPrefix answers an empty list and never nil, so a list without
	// names encodes as [] and never as null.
	_ = json.NewEncoder(w).Encode(tablesResponse{Tables: names})
}

// tableResponse is the table body: the create body's fields with
// $timestamp as the first column, plus the TTL and the tags. Both
// durations are integer milliseconds.
type tableResponse struct {
	Name      string          `json:"name"`
	ShardSize int64           `json:"shard_size"`
	TTL       int64           `json:"ttl"`
	Columns   []columnRequest `json:"columns"`
	Tags      []string        `json:"tags"`
}

// millisOf answers the schema metadata under the key as an integer count
// of milliseconds, or fails when the key is absent or does not parse.
func millisOf(md arrow.Metadata, key string) (int64, error) {
	v, ok := md.GetValue(key)
	if !ok {
		return 0, fmt.Errorf("table schema has no %s", key)
	}
	return strconv.ParseInt(v, 10, 64)
}

// tableResponseOf renders the table in the create vocabulary. It fails
// only on a schema Cluster.Table did not build: a field ColumnOfField
// cannot name, or a duration that is not integer milliseconds.
func tableResponseOf(t qdb.TableInfo) (tableResponse, error) {
	md := t.Schema.Metadata()
	shard, err := millisOf(md, qdb.MetaShardSize)
	if err != nil {
		return tableResponse{}, err
	}
	ttl, err := millisOf(md, qdb.MetaTTL)
	if err != nil {
		return tableResponse{}, err
	}
	// $timestamp is the schema's first field and stays the first column,
	// because every query returns it.
	cols := make([]columnRequest, 0, t.Schema.NumFields())
	for _, f := range t.Schema.Fields() {
		c, err := qdb.ColumnOfField(f)
		if err != nil {
			return tableResponse{}, err
		}
		cols = append(cols, columnRequest{Name: c.Name, Type: c.Type, Symtable: c.Symtable})
	}
	// A table without tags encodes [] and never null.
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	return tableResponse{Name: t.Name, ShardSize: shard, TTL: ttl, Columns: cols, Tags: tags}, nil
}

// handleGetTable answers the table the path names, its columns and its
// tags. A name the cluster does not know is 404, and anything else the
// cluster answered is the caller's 400.
func handleGetTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := qdb.ClusterFrom(ctx).Table(ctx, caller(r), r.PathValue("name"))
	switch {
	case qdb.IsTableNotFound(err):
		writeProblem(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
		return
	}
	body, err := tableResponseOf(t)
	if err != nil {
		// A schema the binding built and this process cannot render is this
		// process's fault.
		writeProblem(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// registerTableRoutes serves the table collection behind the bearer
// middleware. The mux patterns fix method and path.
func registerTableRoutes(mux *http.ServeMux) {
	handleData(mux, "GET "+tablesPath, WithCompression(requireBearer(http.HandlerFunc(handleListTables))))
	handleData(mux, "GET "+tablesPath+"/{name}", WithCompression(requireBearer(http.HandlerFunc(handleGetTable))))
	handleData(mux, "POST "+tablesPath, WithCompression(requireBearer(http.HandlerFunc(handleCreateTable))))
	handleData(mux, "DELETE "+tablesPath+"/{name}", WithCompression(requireBearer(http.HandlerFunc(handleDeleteTable))))
}
