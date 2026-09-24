// The table reader's test helpers and its range; its good path in every
// format is the round trip (roundtrip_test.go), its error rows are errors_test.go's.
package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
