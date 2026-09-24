// The good path of the v2 surface is one round trip against the live qdbd
// fixture, the in-process twin of the e2e flow: generated tables of one
// column list are created over HTTP, read empty, ingested in one body,
// read and queried in every format, deleted, re-created over what the
// delete leaves behind, and deleted again. What went in comes back out:
// each response equals the encoder run directly over the cluster, and the
// direct read passes table.Check against the rows generated, so the bytes
// on the wire carry the rows written.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// ingest posts body as CSV under the URL parameters.
func (s server) ingest(body, params string, headers map[string]string) *httptest.ResponseRecorder {
	if headers == nil {
		headers = map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": encoding.CSVContentType}
	}
	return s.post(rowsPath+"?"+params, body, headers)
}

// ingestBodyOf is the one CSV body that carries every table: the first
// table's CSV whole, then the rows of the others under its header, which
// is theirs too since they share the column list.
func ingestBodyOf(tables []table.Table) string {
	var buf bytes.Buffer
	for i, tbl := range tables {
		csv := table.CSV(tbl)
		if i > 0 {
			csv = csv[bytes.IndexByte(csv, '\n')+1:]
		}
		buf.Write(csv)
	}
	return buf.String()
}

// checkRead reads tbl over the cluster and compares what comes back with
// the rows generated; the fixture's tables fit one batch.
func (s server) checkRead(t *rapid.T, tbl table.Table) {
	t.Helper()
	err := s.c.Read(context.Background(), qdb.User{}, tbl.Name, qdb.ReadOptions{}, func(batches qdb.Batches) error {
		for rec, err := range batches {
			if err != nil {
				return err
			}
			table.Check(t, tbl, rec)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read %s: %v", tbl.Name, err)
	}
}

// checkQuery queries tbl over the cluster and compares what comes back
// with the rows generated: the read-back a query sees, which after an
// async push is ahead of the bulk reader's.
func (s server) checkQuery(t *rapid.T, tbl table.Table) {
	t.Helper()
	rec, err := s.c.Query(context.Background(), qdb.User{}, tbl.Select())
	if err != nil {
		t.Fatalf("query %s: %v", tbl.Name, err)
	}
	if rec == nil {
		t.Fatalf("query %s: no result set", tbl.Name)
	}
	defer rec.Release()
	table.Check(t, tbl, rec)
}

// checkReadFormats reads tbl over HTTP in every format, whole and, when
// picked names columns, under that subset, and compares each body with
// the stream encoder run directly.
func (s server) checkReadFormats(t *rapid.T, tbl table.Table, picked []string) {
	t.Helper()
	cases := []struct {
		params string
		o      qdb.ReadOptions
	}{{"", qdb.ReadOptions{}}}
	if picked != nil {
		cases = append(cases, struct {
			params string
			o      qdb.ReadOptions
		}{"columns=" + url.QueryEscape(strings.Join(picked, ",")), qdb.ReadOptions{Columns: picked}})
	}
	for _, tc := range cases {
		for accept, e := range encoders {
			resp := s.readTable(tbl.Name, tc.params, map[string]string{"Authorization": "Bearer " + s.token, "Accept": accept})
			if resp.Code != http.StatusOK {
				t.Fatalf("read %s ?%s: status %d: %s", accept, tc.params, resp.Code, resp.Body.String())
			}
			if ct := resp.Header().Get("Content-Type"); ct != e.ContentType() {
				t.Fatalf("read %s: Content-Type = %q", accept, ct)
			}
			if want := s.directRead(t, e, tbl.Name, tc.o); !bytes.Equal(resp.Body.Bytes(), want) {
				t.Fatalf("read %s ?%s: body differs from the stream encoder's own bytes", accept, tc.params)
			}
		}
	}
}

// checkQueryFormats queries tbl over HTTP in every format and compares
// each body with the encoder run directly.
func (s server) checkQueryFormats(t *rapid.T, tbl table.Table) {
	t.Helper()
	for accept, e := range encoders {
		resp := s.query(tbl.Select(), map[string]string{"Authorization": "Bearer " + s.token, "Accept": accept})
		if resp.Code != http.StatusOK {
			t.Fatalf("query %s: status %d: %s", accept, resp.Code, resp.Body.String())
		}
		if ct := resp.Header().Get("Content-Type"); ct != e.ContentType() {
			t.Fatalf("query %s: Content-Type = %q", accept, ct)
		}
		if want := s.direct(t, e, tbl.Select()); !bytes.Equal(resp.Body.Bytes(), want) {
			t.Fatalf("query %s: body differs from the encoder's own bytes", accept)
		}
	}
}

// ingestResponseOf decodes the ingest's answer.
func ingestResponseOf(t table.T, resp *httptest.ResponseRecorder) ingestResponse {
	t.Helper()
	var r ingestResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &r); err != nil || resp.Code != http.StatusOK {
		t.Fatalf("ingest: status %d: %s (%v)", resp.Code, resp.Body.String(), err)
	}
	return r
}

// TestRoundtrip: the round trip above, per iteration over one to three generated
// tables of one column list.
func TestRoundtrip(t *testing.T) {
	s := newServer(t)
	rapid.Check(t, func(rt *rapid.T) {
		// 1. the tables: the first drawn whole, the rest of its columns
		first := table.Generate(rt)
		tables := []table.Table{first}
		for range rapid.IntRange(0, 2).Draw(rt, "more tables") {
			tables = append(tables, table.GenerateLike(rt, first))
		}
		wantRows, wantTables := 0, 0
		for _, tbl := range tables {
			wantRows += len(tbl.Index)
			if len(tbl.Index) > 0 {
				wantTables++
			}
		}
		all := []string{"$table", "$timestamp"}
		for _, c := range first.Columns {
			all = append(all, c.Name)
		}
		picked := rapid.Permutation(all).Draw(rt, "order")[:rapid.IntRange(1, len(all)).Draw(rt, "picked")]

		// 2. create each: 201 with its Location
		for _, tbl := range tables {
			table.RemoveOnCleanup(rt, s.c, tbl)
			resp := s.createTable(rt, createBodyOf(tbl))
			if resp.Code != http.StatusCreated || resp.Body.Len() != 0 {
				rt.Fatalf("create %s: status %d: %s", tbl.Name, resp.Code, resp.Body.String())
			}
			if got := resp.Header().Get("Location"); got != tablesPath+"/"+tbl.Name {
				rt.Fatalf("create %s: Location = %q", tbl.Name, got)
			}
		}

		// 3. read the first table empty: the schema alone, in every format,
		// and the schema is the one generated. The others share it, and an
		// empty read through the bulk reader is slow (about a tenth of a
		// second where a filled read is milliseconds), so one table stands
		// for all
		empty := first
		empty.Index = nil
		empty.Columns = slices.Clone(first.Columns)
		for i := range empty.Columns {
			empty.Columns[i].Valid = nil
		}
		s.checkRead(rt, empty)
		s.checkReadFormats(rt, first, nil)

		// 4. ingest every table's rows in one body under a drawn push mode,
		// the default included: the counts answered are the rows generated
		// and the tables that had any
		mode := rapid.SampledFrom([]string{"", "fast", "transactional", "async"}).Draw(rt, "push mode")
		got := ingestResponseOf(rt, s.ingest(ingestBodyOf(tables), "push-mode="+mode, nil))
		if got.Rows != wantRows || got.Tables != wantTables {
			rt.Fatalf("ingest answered %+v, want %d rows in %d tables", got, wantRows, wantTables)
		}

		// 5. read and query each in every format: the rows written are the
		// rows generated, and every wire carries the encoder's own bytes.
		// After an async push a query sees the rows at once and the bulk
		// reader only after the server's flush, so async reads back through
		// the query alone
		for _, tbl := range tables {
			s.checkQuery(rt, tbl)
			s.checkQueryFormats(rt, tbl)
			if mode == "async" {
				continue
			}
			s.checkRead(rt, tbl)
			s.checkReadFormats(rt, tbl, picked)
		}

		// 6. delete each (204), which leaves the symtables; the same create
		// is accepted again over them (201); a second delete is 404
		for _, tbl := range tables {
			if resp := s.deleteTable(tbl.Name); resp.Code != http.StatusNoContent || resp.Body.Len() != 0 {
				rt.Fatalf("delete %s: status %d: %s", tbl.Name, resp.Code, resp.Body.String())
			}
			if resp := s.createTable(rt, createBodyOf(tbl)); resp.Code != http.StatusCreated {
				rt.Fatalf("re-create %s: status %d: %s", tbl.Name, resp.Code, resp.Body.String())
			}
			if resp := s.deleteTable(tbl.Name); resp.Code != http.StatusNoContent {
				rt.Fatalf("delete %s again: status %d: %s", tbl.Name, resp.Code, resp.Body.String())
			}
			if resp := s.deleteTable(tbl.Name); resp.Code != http.StatusNotFound {
				rt.Fatalf("delete %s of nothing: status %d: %s", tbl.Name, resp.Code, resp.Body.String())
			}
		}
	})
}

// TestRoundtripDeduplicated: the same body ingested twice under drop on
// $timestamp reads back once.
func TestRoundtripDeduplicated(t *testing.T) {
	s := newServer(t)
	rapid.Check(t, func(rt *rapid.T) {
		tbl := table.Generate(rt)
		if len(tbl.Index) == 0 {
			return
		}
		table.RemoveOnCleanup(rt, s.c, tbl)
		if resp := s.createTable(rt, createBodyOf(tbl)); resp.Code != http.StatusCreated {
			rt.Fatalf("create: status %d: %s", resp.Code, resp.Body.String())
		}
		query := "deduplication-mode=drop&deduplication-columns=" + url.QueryEscape("$timestamp")
		for range 2 {
			ingestResponseOf(rt, s.ingest(ingestBodyOf([]table.Table{tbl}), query, nil))
		}
		s.checkRead(rt, tbl)
	})
}
