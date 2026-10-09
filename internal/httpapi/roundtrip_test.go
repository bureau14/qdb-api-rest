// The good path of the v2 surface is one round trip against the live qdbd
// fixture, the in-process twin of the e2e flow. The test creates
// generated tables that share one column list over HTTP, reads them
// empty, ingests them in one body of a drawn format under a drawn
// request coding, reads and queries them in every format, deletes them, re-creates them over what the delete leaves
// behind, and deletes them again. What went in comes back out. Each
// response equals the encoder run directly over the cluster, and the
// direct read passes table.Check against the rows generated, so the bytes
// on the wire carry the rows written.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
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

// ingest posts body under the URL parameters, as CSV when no headers
// are given.
func (s server) ingest(body []byte, params string, headers map[string]string) *httptest.ResponseRecorder {
	if headers == nil {
		headers = map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": encoding.CSVContentType}
	}
	return s.post(rowsPath+"?"+params, string(body), headers)
}

// ingestBodyOf is the body in e's format that carries every table. The
// fixture joins the tables into one batch, and the encoder writes it
// out.
func ingestBodyOf(t table.T, e encoding.Encoder, tables []table.Table) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := e.Encode(context.Background(), &buf, table.Body(t, tables...)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// compressedBody is body under coding c, through the server's own
// compressor, which the round trip trusts because the response tests
// have decoded it with the reference decoders.
func compressedBody(t table.T, c coding, body []byte) []byte {
	t.Helper()
	if c == identityCoding {
		return body
	}
	var buf bytes.Buffer
	z := newCompressor(c, &buf)
	if _, err := z.Write(body); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := z.Close(); err != nil {
		t.Fatalf("compress: %v", err)
	}
	return buf.Bytes()
}

// drawnIngest posts every table's rows in one body of a drawn format
// under a drawn request coding, with the push mode as the parameter.
func (s server) drawnIngest(t *rapid.T, tables []table.Table, mode string) *httptest.ResponseRecorder {
	t.Helper()
	contentType := rapid.SampledFrom(slices.Sorted(maps.Keys(decoders))).Draw(t, "content type")
	c := rapid.SampledFrom([]coding{identityCoding, gzipCoding, zstdCoding}).Draw(t, "content coding")
	body := compressedBody(t, c, ingestBodyOf(t, encoders[contentType], tables))
	headers := map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": contentType}
	if c != identityCoding {
		headers["Content-Encoding"] = string(c)
	}
	return s.ingest(body, "push-mode="+mode, headers)
}

// checkRead reads tbl over the cluster and compares what comes back with
// the rows generated. The fixture's tables fit one batch.
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
// with the rows generated. This is the view a query has, which after an
// async push is ahead of what the bulk reader sees (internal/AGENTS.md,
// the ingest).
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

// checkReadFormats reads tbl over HTTP in every format, once whole and,
// when picked is non-nil, once more restricted to those columns, and
// compares each body with the stream encoder run directly.
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

// TestRoundtrip: the round trip above, run per iteration over one to
// three generated tables that share one column list.
func TestRoundtrip(t *testing.T) {
	s := newServer(t)
	rapid.Check(t, func(rt *rapid.T) {
		// 1. the tables: the first is drawn whole, the rest are generated over its columns
		first := table.Generate(rt)
		tables := []table.Table{first}
		for range rapid.IntRange(0, 2).Draw(rt, "more tables") {
			tables = append(tables, table.GenerateLike(rt, first))
		}
		wantRows, wantTables := 0, 0
		for _, tbl := range tables {
			wantRows += tbl.Rows()
			if tbl.Rows() > 0 {
				wantTables++
			}
		}
		all := []string{"$table", "$timestamp"}
		for _, c := range first.Columns {
			all = append(all, c.Name)
		}
		picked := rapid.Permutation(all).Draw(rt, "order")[:rapid.IntRange(1, len(all)).Draw(rt, "picked")]

		// 2. create each table, which answers 201 with its Location
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

		// 3. read the first table empty, which answers the schema alone in
		// every format, and the schema is the one generated. The others share it, and an
		// empty read through the bulk reader is slow (about a tenth of a
		// second where a filled read is milliseconds), so one table stands
		// for all
		empty := first
		empty.Batch = nil
		s.checkRead(rt, empty)
		s.checkReadFormats(rt, first, nil)

		// 4. ingest every table's rows in one body of a drawn format under a
		// drawn request coding and a drawn push mode, the default included.
		// The counts answered are the rows generated and the tables that
		// had any
		mode := rapid.SampledFrom([]string{"", "fast", "transactional", "async"}).Draw(rt, "push mode")
		got := ingestResponseOf(rt, s.drawnIngest(rt, tables, mode))
		if got.Rows != wantRows || got.Tables != wantTables {
			rt.Fatalf("ingest answered %+v, want %d rows in %d tables", got, wantRows, wantTables)
		}

		// 5. read and query each in every format. The rows written are the
		// rows generated, and every format's body carries the encoder's own
		// bytes. After an async push a query sees the rows at once, but the
		// bulk reader sees them only once the server has flushed. Before that
		// it answers none of them, or some twice while the flush runs (qdbd
		// 3.15.0.dev0, both writers), so the async mode is read back through
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

		// 6. delete each (204), which leaves the symtables. The same create
		// is accepted again over them (201), and a second delete is 404
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
		if tbl.Rows() == 0 {
			return
		}
		table.RemoveOnCleanup(rt, s.c, tbl)
		if resp := s.createTable(rt, createBodyOf(tbl)); resp.Code != http.StatusCreated {
			rt.Fatalf("create: status %d: %s", resp.Code, resp.Body.String())
		}
		query := "deduplication-mode=drop&deduplication-columns=" + url.QueryEscape("$timestamp")
		for range 2 {
			ingestResponseOf(rt, s.ingest(ingestBodyOf(rt, encoding.CSV{}, []table.Table{tbl}), query, nil))
		}
		s.checkRead(rt, tbl)
	})
}
