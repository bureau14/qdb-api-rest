// The table routes are pinned against the live qdbd fixture: a generated
// schema is created over HTTP, answers its columns when queried empty,
// conflicts on a second create, and is deleted once; the error rows of
// both routes are checked one by one on the same fixture.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
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

// schemaTypes is what a column's type word is drawn from.
var schemaTypes = []string{"blob", "double", "int64", "string", "symbol", "timestamp"}

// generateCreate draws a create body: a fixture-prefixed name, a shard
// size, and one to five columns over the six types, a symbol naming a
// symtable after its table and column.
func generateCreate(rt *rapid.T) createTableRequest {
	name := "qdbtest_" + rapid.StringMatching(`[a-z]{16}`).Draw(rt, "table")
	shard := rapid.Int64Range(1000, 86_400_000).Draw(rt, "shard size")
	cols := make([]columnRequest, rapid.IntRange(1, 5).Draw(rt, "columns"))
	for i := range cols {
		cols[i] = columnRequest{Name: fmt.Sprintf("c%d", i), Type: rapid.SampledFrom(schemaTypes).Draw(rt, "type")}
		if cols[i].Type == "symbol" {
			cols[i].Symtable = name + "_" + cols[i].Name
		}
	}
	return createTableRequest{Name: name, ShardSize: &shard, Columns: cols}
}

// removeOnCleanup removes whatever a create can leave behind, the table
// and its never-filled symtables, tolerating what is already gone.
func removeOnCleanup(t table.T, c *qdb.Cluster, req createTableRequest) {
	names := []string{req.Name}
	for _, col := range req.Columns {
		if col.Symtable != "" {
			names = append(names, col.Symtable)
		}
	}
	t.Cleanup(func() {
		for _, name := range names {
			err := c.RemoveTable(context.Background(), qdb.User{}, name)
			if err != nil && !errors.Is(err, qdbapi.ErrAliasNotFound) {
				t.Errorf("remove %s: %v", name, err)
			}
		}
	})
}

// selectOf is the query that answers req's columns in order, $timestamp
// first.
func selectOf(req createTableRequest) string {
	names := []string{"$timestamp"}
	for _, c := range req.Columns {
		names = append(names, c.Name)
	}
	return "SELECT " + strings.Join(names, ", ") + " FROM " + req.Name
}

// wireType is the type word a query answers for a schema type word: a
// symbol reads back as a string.
func wireType(schemaType string) string {
	if schemaType == "symbol" {
		return "string"
	}
	return schemaType
}

// TestTableLifecycle: a generated schema is created (201, Location),
// answers exactly its columns when queried empty, conflicts on a second
// create (409), is deleted (204) and is then unknown (404).
func TestTableLifecycle(t *testing.T) {
	s := newServer(t)
	rapid.Check(t, func(rt *rapid.T) {
		req := generateCreate(rt)
		removeOnCleanup(rt, s.c, req)
		resp := s.createTable(rt, req)
		if resp.Code != http.StatusCreated || resp.Body.Len() != 0 {
			rt.Fatalf("create: status %d: %s", resp.Code, resp.Body.String())
		}
		if got := resp.Header().Get("Location"); got != tablesPath+"/"+req.Name {
			rt.Fatalf("Location = %q", got)
		}
		// The empty table answers its schema: every column, in order, under
		// its wire type, with no data.
		q := s.query(selectOf(req), map[string]string{"Authorization": "Bearer " + s.token})
		var result struct {
			Columns []struct {
				Name string `json:"name"`
				Type string `json:"type"`
				Data []any  `json:"data"`
			} `json:"columns"`
		}
		if err := json.Unmarshal(q.Body.Bytes(), &result); err != nil || q.Code != http.StatusOK {
			rt.Fatalf("query: status %d: %s (%v)", q.Code, q.Body.String(), err)
		}
		if len(result.Columns) != len(req.Columns)+1 {
			rt.Fatalf("query answered %d columns: %s", len(result.Columns), q.Body.String())
		}
		for i, c := range req.Columns {
			got := result.Columns[i+1]
			if got.Name != c.Name || got.Type != wireType(c.Type) || len(got.Data) != 0 {
				rt.Fatalf("column %d = %+v, want %s %s", i, got, c.Name, wireType(c.Type))
			}
		}
		if resp := s.createTable(rt, req); resp.Code != http.StatusConflict {
			rt.Fatalf("second create: status %d: %s", resp.Code, resp.Body.String())
		}
		if resp := s.deleteTable(req.Name); resp.Code != http.StatusNoContent || resp.Body.Len() != 0 {
			rt.Fatalf("delete: status %d: %s", resp.Code, resp.Body.String())
		}
		if resp := s.deleteTable(req.Name); resp.Code != http.StatusNotFound {
			rt.Fatalf("second delete: status %d: %s", resp.Code, resp.Body.String())
		}
	})
}

// TestTableRecreatedOverSymtable: a table whose symtables were filled is
// deleted, which leaves the symtables, and the same create is accepted
// again over them.
func TestTableRecreatedOverSymtable(t *testing.T) {
	s := newServer(t)
	shard := int64(86_400_000)
	req := createTableRequest{Name: "qdbtest_recreated", ShardSize: &shard, Columns: []columnRequest{
		{Name: "c0", Type: "symbol", Symtable: "qdbtest_recreated_c0"},
	}}
	removeOnCleanup(t, s.c, req)
	if resp := s.createTable(t, req); resp.Code != http.StatusCreated {
		t.Fatalf("create: status %d: %s", resp.Code, resp.Body.String())
	}
	// One pushed symbol value brings the symtable into existence.
	insert := "INSERT INTO qdbtest_recreated ($timestamp, c0) VALUES (2020-01-01, 'a')"
	if resp := s.query(insert, map[string]string{"Authorization": "Bearer " + s.token}); resp.Code != http.StatusOK {
		t.Fatalf("insert: status %d: %s", resp.Code, resp.Body.String())
	}
	if resp := s.deleteTable(req.Name); resp.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d: %s", resp.Code, resp.Body.String())
	}
	if resp := s.createTable(t, req); resp.Code != http.StatusCreated {
		t.Fatalf("second create: status %d: %s", resp.Code, resp.Body.String())
	}
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
