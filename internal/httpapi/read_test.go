// The table reader's helpers, its range and its error rows; its good
// path in every format is the flow (flow_test.go).
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// readTable gets the rows of name under the query string with headers.
func (s server) readTable(name, query string, headers map[string]string) *httptest.ResponseRecorder {
	return s.send(http.MethodGet, tablesPath+"/"+name+"/rows?"+query, "", headers)
}

// directRead reads name over the cluster and encodes it with e's stream
// path, the bytes the endpoint must match.
func (s server) directRead(t *rapid.T, e encoding.Encoder, name string, o qdb.ReadOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := s.c.Read(context.Background(), qdb.User{}, name, o, func(batches qdb.Batches) error {
		return e.EncodeStream(context.Background(), &buf, batches)
	})
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return buf.Bytes()
}

// TestReadTableRange: a range that covers the table answers what the
// whole read answers; a range before it answers the schema alone.
func TestReadTableRange(t *testing.T) {
	s := newServer(t)
	rapid.Check(t, func(rt *rapid.T) {
		tbl := table.Generate(rt)
		table.Create(rt, s.c, tbl)
		bearer := map[string]string{"Authorization": "Bearer " + s.token, "Accept": encoding.CSVContentType}
		whole := s.readTable(tbl.Name, "", bearer)
		covering := s.readTable(tbl.Name, "start=2020-01-01T00:00:00Z&end=2100-01-01T00:00:00Z", bearer)
		if covering.Code != http.StatusOK || !bytes.Equal(covering.Body.Bytes(), whole.Body.Bytes()) {
			rt.Fatalf("covering range: status %d: %s", covering.Code, covering.Body.String())
		}
		before := s.readTable(tbl.Name, "start=2000-01-01T00:00:00Z&end=2000-01-02T00:00:00Z", bearer)
		header := whole.Body.Bytes()[:bytes.IndexByte(whole.Body.Bytes(), '\n')+1]
		if before.Code != http.StatusOK || !bytes.Equal(before.Body.Bytes(), header) {
			rt.Fatalf("range before the rows: status %d: %q, want the header alone", before.Code, before.Body.String())
		}
	})
}

// TestReadTableErrors: each error row answers its status with a problem
// body.
func TestReadTableErrors(t *testing.T) {
	s := newServer(t)
	tbl := table.Table{Name: "qdbtest_read_errors", Columns: []table.Column{{Name: "c0", Type: qdbapi.TsColumnInt64}}}
	table.Create(t, s.c, tbl)
	bearer := map[string]string{"Authorization": "Bearer " + s.token}
	cases := map[string]struct {
		name, query string
		headers     map[string]string
		status      int
	}{
		"no token":         {tbl.Name, "", nil, http.StatusUnauthorized},
		"no such table":    {"qdbtest_read_none", "", bearer, http.StatusNotFound},
		"unparsable start": {tbl.Name, "start=yesterday&end=2020-01-02T00:00:00Z", bearer, http.StatusBadRequest},
		"start alone":      {tbl.Name, "start=2020-01-01T00:00:00Z", bearer, http.StatusBadRequest},
		"end not after":    {tbl.Name, "start=2020-01-02T00:00:00Z&end=2020-01-01T00:00:00Z", bearer, http.StatusBadRequest},
		"unknown column":   {tbl.Name, "columns=nope", bearer, http.StatusBadRequest},
	}
	for name, tc := range cases {
		resp := s.readTable(tc.name, tc.query, tc.headers)
		if resp.Code != tc.status {
			t.Errorf("%s: status %d, want %d: %s", name, resp.Code, tc.status, resp.Body.String())
		}
		var p problem
		if err := json.Unmarshal(resp.Body.Bytes(), &p); err != nil || p.Status != tc.status || p.Title != http.StatusText(tc.status) {
			t.Errorf("%s: problem body %s (%v)", name, resp.Body.String(), err)
		}
	}
}
