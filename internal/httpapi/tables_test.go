// The table routes' test helpers; their good path is the round
// trip (roundtrip_test.go), their error rows are errors_test.go's.
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

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
