// Every error row of the v2 routes is one table: a request, the status
// it answers, and the challenge where the status carries one. Each row
// answers a problem body whose status repeats the status line and whose
// title is its standard text. The good path is the flow (flow_test.go).
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// errorRow is one request and what it must answer.
type errorRow struct {
	method, path, body string
	headers            map[string]string
	status             int
	challenge          string // WWW-Authenticate, where the status has one
}

// TestErrorRows: every row answers its status, its challenge and a
// problem body. Two fixed tables of one column each, an int64 and a
// double, give the reader's and the ingest's rows something to name.
func TestErrorRows(t *testing.T) {
	s := newServer(t)
	a := table.Table{Name: "qdbtest_errors_a", Columns: []table.Column{{Name: "c0", Type: qdbapi.TsColumnInt64}}}
	b := table.Table{Name: "qdbtest_errors_b", Columns: []table.Column{{Name: "c0", Type: qdbapi.TsColumnDouble}}}
	table.Create(t, s.c, a)
	table.Create(t, s.c, b)
	bearer := map[string]string{"Authorization": "Bearer " + s.token}
	jsonBody := map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": "application/json"}
	csvBody := map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": encoding.CSVContentType}
	read := func(name, query string) string { return tablesPath + "/" + name + "/rows?" + query }
	ingest := func(query string) string { return rowsPath + "?" + query }
	csvHeader := "$table,$timestamp,c0\n"
	rows := map[string]errorRow{
		// the query
		"query: invalid query": {http.MethodPost, "/api/v2/query", "NOT A QUERY", bearer, http.StatusBadRequest, ""},
		"query: json body":     {http.MethodPost, "/api/v2/query", `{"query":"SELECT 1"}`, jsonBody, http.StatusUnsupportedMediaType, ""},
		"query: no token":      {http.MethodPost, "/api/v2/query", "SELECT 1", nil, http.StatusUnauthorized, "Bearer"},
		"query: bad token":     {http.MethodPost, "/api/v2/query", "SELECT 1", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized, `Bearer error="invalid_token"`},
		// the tables
		"create: text body":               {http.MethodPost, tablesPath, `{}`, map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": "text/plain"}, http.StatusUnsupportedMediaType, ""},
		"create: undecodable":             {http.MethodPost, tablesPath, `{"name":`, jsonBody, http.StatusBadRequest, ""},
		"create: no shard_size":           {http.MethodPost, tablesPath, `{"name":"qdbtest_err","columns":[{"name":"c","type":"int64"}]}`, jsonBody, http.StatusBadRequest, ""},
		"create: fractional shard_size":   {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":1.5,"columns":[{"name":"c","type":"int64"}]}`, jsonBody, http.StatusBadRequest, ""},
		"create: shard_size out of range": {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":9223372036854775807,"columns":[{"name":"c","type":"int64"}]}`, jsonBody, http.StatusBadRequest, ""},
		"create: unknown type":            {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":1000,"columns":[{"name":"c","type":"count"}]}`, jsonBody, http.StatusBadRequest, ""},
		"create: symbol without symtable": {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":1000,"columns":[{"name":"c","type":"symbol"}]}`, jsonBody, http.StatusBadRequest, ""},
		"create: symtable on a string":    {http.MethodPost, tablesPath, `{"name":"qdbtest_err","shard_size":1000,"columns":[{"name":"c","type":"string","symtable":"s"}]}`, jsonBody, http.StatusBadRequest, ""},
		"create: taken name":              {http.MethodPost, tablesPath, `{"name":"qdbtest_errors_a","shard_size":1000,"columns":[{"name":"c0","type":"int64"}]}`, jsonBody, http.StatusConflict, ""},
		"create: no token":                {http.MethodPost, tablesPath, `{}`, map[string]string{"Content-Type": "application/json"}, http.StatusUnauthorized, "Bearer"},
		"delete: no token":                {http.MethodDelete, tablesPath + "/qdbtest_err", "", nil, http.StatusUnauthorized, "Bearer"},
		"delete: no such table":           {http.MethodDelete, tablesPath + "/qdbtest_err", "", bearer, http.StatusNotFound, ""},
		// the reader
		"read: no token":         {http.MethodGet, read(a.Name, ""), "", nil, http.StatusUnauthorized, "Bearer"},
		"read: no such table":    {http.MethodGet, read("qdbtest_read_none", ""), "", bearer, http.StatusNotFound, ""},
		"read: unparsable start": {http.MethodGet, read(a.Name, "start=yesterday&end=2020-01-02T00:00:00Z"), "", bearer, http.StatusBadRequest, ""},
		"read: start alone":      {http.MethodGet, read(a.Name, "start=2020-01-01T00:00:00Z"), "", bearer, http.StatusBadRequest, ""},
		"read: end not after":    {http.MethodGet, read(a.Name, "start=2020-01-02T00:00:00Z&end=2020-01-01T00:00:00Z"), "", bearer, http.StatusBadRequest, ""},
		"read: unknown column":   {http.MethodGet, read(a.Name, "columns=nope"), "", bearer, http.StatusBadRequest, ""},
		// the ingest
		"ingest: no token":                 {http.MethodPost, ingest(""), csvHeader, map[string]string{"Content-Type": encoding.CSVContentType}, http.StatusUnauthorized, "Bearer"},
		"ingest: json body":                {http.MethodPost, ingest(""), `[]`, jsonBody, http.StatusUnsupportedMediaType, ""},
		"ingest: over the cap":             {http.MethodPost, ingest(""), csvHeader + strings.Repeat("qdbtest_errors_a,2020-01-01T00:00:00.000000000Z,1\n", maxIngestBytes/48+1), csvBody, http.StatusRequestEntityTooLarge, ""},
		"ingest: unknown push mode":        {http.MethodPost, ingest("push-mode=eventually"), csvHeader, csvBody, http.StatusBadRequest, ""},
		"ingest: unknown dedup mode":       {http.MethodPost, ingest("deduplication-mode=merge&deduplication-columns=c0"), csvHeader, csvBody, http.StatusBadRequest, ""},
		"ingest: dedup without columns":    {http.MethodPost, ingest("deduplication-mode=drop"), csvHeader, csvBody, http.StatusBadRequest, ""},
		"ingest: columns without a mode":   {http.MethodPost, ingest("deduplication-columns=c0"), csvHeader, csvBody, http.StatusBadRequest, ""},
		"ingest: no $table":                {http.MethodPost, ingest(""), "$timestamp,c0\n", csvBody, http.StatusBadRequest, ""},
		"ingest: no $timestamp":            {http.MethodPost, ingest(""), "$table,c0\n", csvBody, http.StatusBadRequest, ""},
		"ingest: unknown column":           {http.MethodPost, ingest(""), "$table,$timestamp,nope\nqdbtest_errors_a,2020-01-01T00:00:00Z,1\n", csvBody, http.StatusBadRequest, ""},
		"ingest: short record":             {http.MethodPost, ingest(""), csvHeader + "qdbtest_errors_a,2020-01-01T00:00:00Z\n", csvBody, http.StatusBadRequest, ""},
		"ingest: unparsable field":         {http.MethodPost, ingest(""), csvHeader + "qdbtest_errors_a,2020-01-01T00:00:00Z,one\n", csvBody, http.StatusBadRequest, ""},
		"ingest: empty $timestamp":         {http.MethodPost, ingest(""), csvHeader + "qdbtest_errors_a,,1\n", csvBody, http.StatusBadRequest, ""},
		"ingest: tables of differing type": {http.MethodPost, ingest(""), csvHeader + "qdbtest_errors_a,2020-01-01T00:00:00Z,1\nqdbtest_errors_b,2020-01-01T00:00:00Z,1\n", csvBody, http.StatusBadRequest, ""},
		"ingest: no such table":            {http.MethodPost, ingest(""), csvHeader + "qdbtest_errors_none,2020-01-01T00:00:00Z,1\n", csvBody, http.StatusNotFound, ""},
	}
	for name, tc := range rows {
		resp := s.send(tc.method, tc.path, tc.body, tc.headers)
		if resp.Code != tc.status {
			t.Errorf("%s: status %d, want %d: %s", name, resp.Code, tc.status, resp.Body.String())
		}
		if got := resp.Header().Get("WWW-Authenticate"); got != tc.challenge {
			t.Errorf("%s: WWW-Authenticate = %q, want %q", name, got, tc.challenge)
		}
		var p problem
		if err := json.Unmarshal(resp.Body.Bytes(), &p); err != nil || p.Status != tc.status || p.Title != http.StatusText(tc.status) {
			t.Errorf("%s: problem body %s (%v)", name, resp.Body.String(), err)
		}
	}
}
