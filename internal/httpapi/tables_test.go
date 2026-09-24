// The table routes' helpers and error rows; their good path is the flow
// (flow_test.go).
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// send is post for any method.
func (s server) send(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(s.ctx, method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp := httptest.NewRecorder()
	s.handler.ServeHTTP(resp, req)
	return resp
}

// createTable posts req as the create body.
func (s server) createTable(t table.T, req createTableRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return s.post(tablesPath, string(body), map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": "application/json"})
}

// deleteTable deletes the table name.
func (s server) deleteTable(name string) *httptest.ResponseRecorder {
	return s.send(http.MethodDelete, tablesPath+"/"+name, "", map[string]string{"Authorization": "Bearer " + s.token})
}

// typeWords is the schema vocabulary of the binding's column types.
var typeWords = map[qdbapi.TsColumnType]string{
	qdbapi.TsColumnBlob:      "blob",
	qdbapi.TsColumnDouble:    "double",
	qdbapi.TsColumnInt64:     "int64",
	qdbapi.TsColumnString:    "string",
	qdbapi.TsColumnSymbol:    "symbol",
	qdbapi.TsColumnTimestamp: "timestamp",
}

// createBodyOf is the create body that describes tbl, sharded by a day.
func createBodyOf(tbl table.Table) createTableRequest {
	shard := int64(86_400_000)
	cols := make([]columnRequest, len(tbl.Columns))
	for i, c := range tbl.Columns {
		cols[i] = columnRequest{Name: c.Name, Type: typeWords[c.Type], Symtable: c.Symtable}
	}
	return createTableRequest{Name: tbl.Name, ShardSize: &shard, Columns: cols}
}

// TestTableErrors: each error row of the two routes answers its status
// with a problem body.
func TestTableErrors(t *testing.T) {
	s := newServer(t)
	bearer := map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": "application/json"}
	cases := map[string]struct {
		method, path, body string
		headers            map[string]string
		status             int
	}{
		"text body":               {http.MethodPost, tablesPath, `{}`, map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		"undecodable":             {http.MethodPost, tablesPath, `{"name":`, bearer, http.StatusBadRequest},
		"no shard_size":           {http.MethodPost, tablesPath, `{"name":"qdbtest_err","columns":[{"name":"c","type":"int64"}]}`, bearer, http.StatusBadRequest},
		"fractional shard_size":   {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":1.5,"columns":[{"name":"c","type":"int64"}]}`, bearer, http.StatusBadRequest},
		"shard_size out of range": {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":9223372036854775807,"columns":[{"name":"c","type":"int64"}]}`, bearer, http.StatusBadRequest},
		"unknown type":            {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":1000,"columns":[{"name":"c","type":"count"}]}`, bearer, http.StatusBadRequest},
		"symbol without symtable": {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":1000,"columns":[{"name":"c","type":"symbol"}]}`, bearer, http.StatusBadRequest},
		"symtable on a string":    {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":1000,"columns":[{"name":"c","type":"string","symtable":"s"}]}`, bearer, http.StatusBadRequest},
		"create without a token":  {http.MethodPost, tablesPath, `{}`, map[string]string{"Content-Type": "application/json"}, http.StatusUnauthorized},
		"delete without a token":  {http.MethodDelete, tablesPath + "/qdbtest_err", "", nil, http.StatusUnauthorized},
		"delete of no table":      {http.MethodDelete, tablesPath + "/qdbtest_err", "", bearer, http.StatusNotFound},
	}
	for name, tc := range cases {
		resp := s.send(tc.method, tc.path, tc.body, tc.headers)
		if resp.Code != tc.status {
			t.Errorf("%s: status %d, want %d: %s", name, resp.Code, tc.status, resp.Body.String())
		}
		var p problem
		if err := json.Unmarshal(resp.Body.Bytes(), &p); err != nil || p.Status != tc.status || p.Title != http.StatusText(tc.status) {
			t.Errorf("%s: problem body %s (%v)", name, resp.Body.String(), err)
		}
	}
}
