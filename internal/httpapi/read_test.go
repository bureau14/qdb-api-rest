// This file holds the table reader's test helpers and its range test.
// Its good path in every format is the round trip (roundtrip_test.go),
// and its error rows live in errors_test.go.
package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// readTable gets the rows of the named table with the URL parameters
// and the headers.
func (s server) readTable(name, params string, headers map[string]string) *httptest.ResponseRecorder {
	return s.send(http.MethodGet, tablesPath+"/"+name+"/rows?"+params, "", headers)
}

// rangeParams renders the span of the table's rows as the start and end
// URL parameters, in RFC 3339 with nanoseconds as parseTime reads it.
func rangeParams(tbl table.Table) string {
	start, end := tbl.Range()
	return "start=" + url.QueryEscape(start.Format(time.RFC3339Nano)) + "&end=" + url.QueryEscape(end.Format(time.RFC3339Nano))
}

// directRead reads the named table over the cluster and encodes it with
// the encoder's stream path. The endpoint must answer the same bytes.
func (s server) directRead(t *rapid.T, e encoding.Encoder, name string, o qdb.ReadOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := s.c.Read(context.Background(), s.user, name, o, func(batches qdb.Batches) error {
		return e.EncodeStream(context.Background(), &buf, batches)
	})
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return buf.Bytes()
}

// TestReadTableRange: on each cluster, a range that covers the table
// answers what the whole read answers, and a range before it answers the
// schema alone.
func TestReadTableRange(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		s := newServer(t, f)
		rapid.Check(t, func(rt *rapid.T) {
			tbl := table.Generate(rt)
			table.CreateAs(rt, s.c, s.user, tbl)
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
	})
}
