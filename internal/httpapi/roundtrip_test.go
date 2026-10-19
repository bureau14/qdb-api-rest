// The good path of the v2 surface is one round trip against the live qdbd
// fixture, the in-process twin of the e2e flow. It runs once per cluster,
// as the cluster's user, anonymous on the insecure one. The test logs in,
// refreshes, reads its session and logs out, then under the refreshed
// token creates generated tables that share one column list over HTTP, reads them
// empty, ingests them in one body of a drawn format under a drawn
// request coding, reads them, queries them and reads them through DuckDB
// behind #sql on the query route in every format, deletes them, re-creates them over what the
// delete leaves behind, and deletes them again. What went in comes back
// out. Each response equals the encoder run directly over the cluster,
// and each direct read passes table.Check against the rows generated, so
// the bytes on the wire carry the rows written, and the DuckDB path and
// the native path answer the same rows.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/build"
	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
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
	err := s.c.Read(context.Background(), s.user, tbl.Name, qdb.ReadOptions{}, func(batches qdb.Batches) error {
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
	rec, err := s.c.Query(context.Background(), s.user, tbl.Select())
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

// directDuckDB runs q on DuckDB over the cluster and encodes the stream
// with e. The endpoint must answer these exact bytes.
func (s server) directDuckDB(t *rapid.T, e encoding.Encoder, q string) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := s.c.Duck(context.Background(), s.user, q, func(batches qdb.Batches) error {
		return e.EncodeStream(context.Background(), &buf, batches)
	})
	if err != nil {
		t.Fatalf("%s %s: %v", e.ContentType(), q, err)
	}
	return buf.Bytes()
}

// checkDuckDB reads tbl through DuckDB over the cluster and compares what
// comes back with the rows generated, so the DuckDB path answers the rows
// the native read answers. The fixture's tables fit one batch.
func (s server) checkDuckDB(t *rapid.T, tbl table.Table) {
	t.Helper()
	err := s.c.Duck(context.Background(), s.user, tbl.SelectSQL(), func(batches qdb.Batches) error {
		for rec, err := range batches {
			if err != nil {
				return err
			}
			table.Check(t, tbl, rec)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("duckdb %s: %v", tbl.Name, err)
	}
}

// sameArrowStream reports whether two Arrow streams carry equal schemas
// and equal batches, null slots aside.
func sameArrowStream(t *rapid.T, got, want []byte) bool {
	t.Helper()
	g, err := ipc.NewReader(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("arrow body: %v", err)
	}
	defer g.Release()
	w, err := ipc.NewReader(bytes.NewReader(want))
	if err != nil {
		t.Fatalf("arrow oracle: %v", err)
	}
	defer w.Release()
	if !g.Schema().Equal(w.Schema()) {
		return false
	}
	for w.Next() {
		if !g.Next() || !array.RecordEqual(g.RecordBatch(), w.RecordBatch()) {
			return false
		}
	}
	return !g.Next() && g.Err() == nil && w.Err() == nil
}

// checkDuckDBFormats posts tbl's DuckDB select behind the #sql prefix to
// /api/v2/query in every format and compares each body with directDuckDB
// over the select alone.
func (s server) checkDuckDBFormats(t *rapid.T, tbl table.Table) {
	t.Helper()
	for accept, e := range encoders {
		resp := s.post("/api/v2/query", "#sql "+tbl.SelectSQL(), map[string]string{"Authorization": "Bearer " + s.token, "Accept": accept})
		if resp.Code != http.StatusOK {
			t.Fatalf("duckdb %s: status %d: %s", accept, resp.Code, resp.Body.String())
		}
		if ct := resp.Header().Get("Content-Type"); ct != e.ContentType() {
			t.Fatalf("duckdb %s: Content-Type = %q", accept, ct)
		}
		// The oracle runs the stripped text on DuckDB directly, so equal
		// bytes prove that the prefix reached DuckDB and the rest of the text
		// reached it unchanged. DuckDB leaves the value bytes under a null
		// slot undefined, and the Arrow stream carries the buffers as they
		// are, so two runs of one query differ there. The Arrow body is
		// compared decoded, and the text formats, which write no value for a
		// null, byte for byte.
		want := s.directDuckDB(t, e, tbl.SelectSQL())
		same := bytes.Equal(resp.Body.Bytes(), want)
		if accept == encoding.ArrowContentType {
			same = sameArrowStream(t, resp.Body.Bytes(), want)
		}
		if !same {
			t.Fatalf("duckdb %s: body differs from the stream encoder's own output", accept)
		}
	}
}

// grantOf decodes a 200 token response.
func grantOf(t table.T, step string, resp *httptest.ResponseRecorder) tokenResponse {
	t.Helper()
	var tr tokenResponse
	if resp.Code != http.StatusOK || json.Unmarshal(resp.Body.Bytes(), &tr) != nil || tr.AccessToken == "" || tr.RefreshToken == "" || tr.TokenType != "Bearer" {
		t.Fatalf("%s: status %d: %s", step, resp.Code, resp.Body.String())
	}
	return tr
}

// authRoundTrip logs in as the server's user, refreshes, reads the
// session under the refreshed access token and logs out, and returns the
// refreshed access token. The user is anonymous on the insecure cluster.
// The refreshed token carries the login's session, and the session
// answers that token's claims, this user, this cluster as secured says
// the fixture is, and this build.
func (s server) authRoundTrip(t *rapid.T, secured bool) string {
	t.Helper()
	jsonBody := map[string]string{"Content-Type": "application/json"}
	body := fmt.Sprintf(`{"username":%q,"secret_key":%q}`, s.user.Username, s.user.SecretKey)
	login := grantOf(t, "login", s.post("/api/v2/auth/login", body, jsonBody))
	refreshed := grantOf(t, "refresh", s.post("/api/v2/auth/refresh", `{"refresh_token":"`+login.RefreshToken+`"}`, jsonBody))
	first, err := s.tk.Verify(login.AccessToken)
	if err != nil {
		t.Fatalf("login token: %v", err)
	}
	next, err := s.tk.Verify(refreshed.AccessToken)
	if err != nil {
		t.Fatalf("refreshed token: %v", err)
	}
	if next.Typ != "access" || next.SessionID != first.SessionID || next.AuthTime != first.AuthTime || next.JTI == first.JTI {
		t.Fatalf("refreshed claims %+v do not continue %+v", next, first)
	}
	bearer := map[string]string{"Authorization": "Bearer " + refreshed.AccessToken}
	resp := s.send(http.MethodGet, "/api/v2/session", "", bearer)
	var got sessionResponse
	if resp.Code != http.StatusOK || json.Unmarshal(resp.Body.Bytes(), &got) != nil {
		t.Fatalf("session: status %d: %s", resp.Code, resp.Body.String())
	}
	want := sessionResponse{
		Username: s.user.Username,
		Cluster:  sessionCluster{URI: s.c.URI(), Secured: secured},
		Token:    sessionToken{Typ: "access", IssuedAt: next.IssuedAt, ExpiresAt: next.ExpiresAt, JTI: next.JTI, AuthTime: next.AuthTime},
		Server:   sessionServer{Version: build.Version},
	}
	if got != want {
		t.Fatalf("session: got %+v, want %+v", got, want)
	}
	if resp := s.send(http.MethodPost, "/api/v2/auth/logout", "", bearer); resp.Code != http.StatusNoContent || resp.Body.Len() != 0 {
		t.Fatalf("logout: status %d: %s", resp.Code, resp.Body.String())
	}
	return refreshed.AccessToken
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

// TestRoundtrip: the round trip above, on each cluster, run per iteration
// over one to three generated tables that share one column list.
func TestRoundtrip(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		s := newServer(t, f)
		rapid.Check(t, func(rt *rapid.T) {
			// 1. the auth steps: login, refresh, session, logout, and every
			// later step runs under the refreshed access token, so the round
			// trip proves a refreshed token is a working token on every route.
			// The logout is a step here and not in the shell round trip,
			// because its 204 is all there is to observe
			s.token = s.authRoundTrip(rt, f.Spec.Secured)

			// 2. the tables: the first is drawn whole, the rest are generated over its columns
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

			// 3. create each table, which answers 201 with its Location
			for _, tbl := range tables {
				table.RemoveOnCleanupAs(rt, s.c, s.user, tbl)
				resp := s.createTable(rt, createBodyOf(tbl))
				if resp.Code != http.StatusCreated || resp.Body.Len() != 0 {
					rt.Fatalf("create %s: status %d: %s", tbl.Name, resp.Code, resp.Body.String())
				}
				if got := resp.Header().Get("Location"); got != tablesPath+"/"+tbl.Name {
					rt.Fatalf("create %s: Location = %q", tbl.Name, got)
				}
			}

			// 4. read the first table empty, which answers the schema alone in
			// every format, and the schema is the one generated. The others share it, and an
			// empty read through the bulk reader is slow (about a tenth of a
			// second where a filled read is milliseconds), so one table stands
			// for all
			empty := first
			empty.Batch = nil
			s.checkRead(rt, empty)
			s.checkReadFormats(rt, first, nil)

			// 5. ingest every table's rows in one body of a drawn format under a
			// drawn request coding and a drawn push mode, the default included.
			// The counts answered are the rows generated and the tables that
			// had any
			mode := rapid.SampledFrom([]string{"", "fast", "transactional", "async"}).Draw(rt, "push mode")
			got := ingestResponseOf(rt, s.drawnIngest(rt, tables, mode))
			if got.Rows != wantRows || got.Tables != wantTables {
				rt.Fatalf("ingest answered %+v, want %d rows in %d tables", got, wantRows, wantTables)
			}

			// 6. read, query and read through DuckDB behind #sql on the query
			// route, each in every format. The rows written are the rows
			// generated, and every format's body carries the encoder's own
			// bytes. After an async push a query sees
			// the rows at once, but the bulk reader sees them only once the
			// server has flushed. Before that it answers none of them, or some
			// twice while the flush runs (qdbd 3.15.0.dev0, both writers), so the
			// async mode is read back through the query alone. qdb-duck scans
			// through the bulk reader, so the DuckDB read is skipped with it
			for _, tbl := range tables {
				s.checkQuery(rt, tbl)
				s.checkQueryFormats(rt, tbl)
				if mode == "async" {
					continue
				}
				s.checkRead(rt, tbl)
				s.checkReadFormats(rt, tbl, picked)
				s.checkDuckDB(rt, tbl)
				s.checkDuckDBFormats(rt, tbl)
			}

			// 7. delete each (204), which leaves the symtables. The same create
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
	})
}

// TestRoundtripDeduplicated: on each cluster, the same body ingested
// twice under drop on $timestamp reads back once.
func TestRoundtripDeduplicated(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		s := newServer(t, f)
		rapid.Check(t, func(rt *rapid.T) {
			tbl := table.Generate(rt)
			if tbl.Rows() == 0 {
				return
			}
			table.RemoveOnCleanupAs(rt, s.c, s.user, tbl)
			if resp := s.createTable(rt, createBodyOf(tbl)); resp.Code != http.StatusCreated {
				rt.Fatalf("create: status %d: %s", resp.Code, resp.Body.String())
			}
			query := "deduplication-mode=drop&deduplication-columns=" + url.QueryEscape("$timestamp")
			for range 2 {
				ingestResponseOf(rt, s.ingest(ingestBodyOf(rt, encoding.CSV{}, []table.Table{tbl}), query, nil))
			}
			s.checkRead(rt, tbl)
		})
	})
}
