// The good path of the v2 surface is one round trip against the live
// qdbd fixture, the in-process twin of the e2e flow. It runs once per
// cluster as the cluster's user, who is anonymous on the insecure one.
// The test logs in, refreshes, reads its session and logs out. Under the
// refreshed token it then creates, over HTTP, generated tables that
// share one column list, and reads them empty. It ingests them in one
// body of a drawn format under a drawn request coding. It reads them,
// queries them, and reads them through DuckDB behind #sql on the query
// route, each in every format. It deletes them, re-creates them over
// what the delete leaves behind, and deletes them again. Each response
// equals the encoder run directly over the cluster, and each direct read
// passes table.Check against the rows generated. So the bytes on the
// wire carry the rows written, and the DuckDB path and the native path
// answer the same rows.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/build"
	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	qdbflight "github.com/bureau14/qdb-api-rest/internal/qdbtest/flight"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// ingest posts the body with the URL parameters, as CSV when headers is
// nil.
func (s server) ingest(body []byte, params string, headers map[string]string) *httptest.ResponseRecorder {
	if headers == nil {
		headers = map[string]string{"Authorization": "Bearer " + s.token, "Content-Type": encoding.CSVContentType}
	}
	return s.post(rowsPath+"?"+params, string(body), headers)
}

// one wraps a batch in a sequence of that batch alone.
func one(rec arrow.RecordBatch) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) { yield(rec, nil) }
}

// ingestBodyOf is the body in the encoder's format that carries every
// table. The fixture joins the tables into one batch, and the encoder
// writes it as a stream of that batch alone.
func ingestBodyOf(t table.T, e encoding.Encoder, tables []table.Table) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := e.EncodeStream(context.Background(), &buf, one(table.Body(t, tables...))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// compressedBody compresses the body in the coding with the server's
// own compressor. The round trip trusts that compressor, because the
// response tests decode its output with the reference decoders.
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

// checkRead reads the table over the cluster within the span of its rows
// and compares the result with the rows generated. The read is bounded
// because an unbounded read of a table with no rows probes every shard
// of a forever range. The fixture's tables fit one batch.
func (s server) checkRead(t *rapid.T, tbl table.Table) {
	t.Helper()
	start, end := tbl.Range()
	err := s.c.Read(context.Background(), s.user, tbl.Name, qdb.ReadOptions{Start: start, End: end}, func(batches qdb.Batches) error {
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

// checkQuery queries the table over the cluster and compares the result
// with the rows generated. After an async push a query sees rows before
// the bulk reader does (internal/AGENTS.md, the ingest).
func (s server) checkQuery(t *rapid.T, tbl table.Table) {
	t.Helper()
	err := s.c.Query(context.Background(), s.user, tbl.Select(), func(seq qdb.Batches) error {
		for rec, err := range seq {
			if err != nil {
				return err
			}
			table.Check(t, tbl, rec)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("query %s: %v", tbl.Name, err)
	}
}

// checkReadFormats reads the table over HTTP in every format within the
// span of its rows and compares each body with the stream encoder run
// directly. It reads every column once, and when picked is non-nil, it
// reads the picked columns once more. The reads are bounded as in
// checkRead.
func (s server) checkReadFormats(t *rapid.T, tbl table.Table, picked []string) {
	t.Helper()
	start, end := tbl.Range()
	cases := []struct {
		params string
		o      qdb.ReadOptions
	}{{rangeParams(tbl), qdb.ReadOptions{Start: start, End: end}}}
	if picked != nil {
		cases = append(cases, struct {
			params string
			o      qdb.ReadOptions
		}{rangeParams(tbl) + "&columns=" + url.QueryEscape(strings.Join(picked, ",")), qdb.ReadOptions{Columns: picked, Start: start, End: end}})
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

// checkQueryFormats queries the table over HTTP in every format and
// compares each body with the encoder run directly.
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

// directDuckDB runs the query on DuckDB over the cluster and encodes the
// stream with the encoder. The endpoint must answer the same bytes.
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

// checkDuckDB reads the table through DuckDB over the cluster and
// compares the result with the rows generated, so the DuckDB path
// answers the rows the native read answers. The fixture's tables fit
// one batch.
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

// checkDuckDBFormats posts the table's DuckDB select behind the #sql
// prefix to /api/v2/query in every format and compares each body with
// directDuckDB over the select alone.
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
		// bytes prove that the prefix routed the query to DuckDB and the rest
		// of the text reached it unchanged. DuckDB leaves the value bytes
		// under a null slot undefined, and the Arrow stream carries the
		// buffers as they are, so two runs of one query differ there. The
		// test compares the Arrow body decoded, and the text formats, which
		// write no value for a null, byte for byte.
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

// checkFlight runs the query over Flight under the server's token. It
// compares the result with the rows generated and with the query
// route's Arrow body for the same query, so both planes answer the same
// table (ADR-0018, Decision 14).
func (s server) checkFlight(t *rapid.T, tbl table.Table, q string) {
	t.Helper()
	// The Flight result is checked against two oracles in a fixed order,
	// under the HTTP token:
	//
	//  1. the Flight result joined into one batch, because the engines cut
	//     the stream differently;
	//  2. the rows generated, column by column by name;
	//  3. the Arrow body of POST /api/v2/query for the same query, read
	//     into one batch and compared by array equality. Every text format
	//     is already proven equal to the encoder over these batches, so
	//     this comparison makes Flight equal to every HTTP format.
	//
	// The Flight bearer is the refreshed HTTP access token, because one
	// token opens both planes (docs/brief.md, Authentication), and the
	// round trip proves that. A Flight Handshake per iteration was
	// rejected, because TestHandshake already proves the handshake.
	ctx := qdbflight.Bearer(context.Background(), s.token)

	// 1. the Flight result joined into one batch, because the native engine
	//    sends model.BatchRows rows per message and DuckDB one vector
	got := qdbflight.Execute(t, s.flight, ctx, q)
	defer got.Release()

	// 2. the rows generated, column by column by name
	table.Check(t, tbl, got)

	// 3. the query route's Arrow body for the query, compared by array
	//    equality, which reads no byte under a null slot. Decoding every
	//    text format back was rejected, because the decoders read the
	//    ingest dialect and drop $table
	resp := s.query(q, map[string]string{"Authorization": "Bearer " + s.token, "Accept": encoding.ArrowContentType})
	if resp.Code != http.StatusOK {
		t.Fatalf("query arrow for flight: status %d: %s", resp.Code, resp.Body.String())
	}
	want := qdbflight.FromIPC(t, resp.Body)
	defer want.Release()
	if !qdbflight.Same(got, want) {
		t.Fatalf("flight %s differs from the query route's Arrow body:\n%v\n%v", q, got, want)
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
// The refreshed token carries the login's session. The session route
// answers that token's claims, this user, this cluster with the
// fixture's secured flag, and this build.
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
			// 1. log in, refresh, read the session and log out. Every later step
			// runs under the refreshed access token, so the round trip proves
			// that a refreshed token works on every route. The logout is a step
			// here and not in the shell round trip, because its 204 is all there
			// is to observe
			s.token = s.authRoundTrip(rt, f.Spec.Secured)

			// 2. the tables: the first is drawn whole, and the rest are generated
			// over its columns
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

			// 4. read the first table empty. It answers the schema alone in every
			// format, and the schema is the one generated. The other tables share
			// the schema, so one table stands for all. The read is bounded like
			// every read here, and a table with no rows spans one nanosecond at
			// the index start
			empty := first
			empty.Batch = nil
			s.checkRead(rt, empty)
			s.checkReadFormats(rt, first, nil)

			// 5. ingest every table's rows in one body of a drawn format under a
			// drawn request coding and a drawn push mode, the default included.
			// The answer counts the rows generated and the tables that had any
			mode := rapid.SampledFrom([]string{"", "fast", "transactional", "async"}).Draw(rt, "push mode")
			got := ingestResponseOf(rt, s.drawnIngest(rt, tables, mode))
			if got.Rows != wantRows || got.Tables != wantTables {
				rt.Fatalf("ingest answered %+v, want %d rows in %d tables", got, wantRows, wantTables)
			}

			// 6. read, query, and read through DuckDB behind #sql on the query
			// route, each in every format. The rows written are the rows
			// generated, and every format's body carries the encoder's own bytes.
			// Both engines are also read over Flight, and the #sql read is
			// skipped along with the DuckDB checks. After an async push a query
			// sees the rows at once, but the bulk reader sees them only after the
			// server has flushed. Until then it answers none of them, or some
			// twice while the flush runs (qdbd 3.15.0.dev0, both writers), so an
			// async push is read back through the query only. qdb-duck scans
			// through the bulk reader, so the DuckDB reads are skipped too
			for _, tbl := range tables {
				s.checkQuery(rt, tbl)
				s.checkQueryFormats(rt, tbl)
				s.checkFlight(rt, tbl, tbl.Select())
				if mode == "async" {
					continue
				}
				s.checkRead(rt, tbl)
				s.checkReadFormats(rt, tbl, picked)
				s.checkDuckDB(rt, tbl)
				s.checkDuckDBFormats(rt, tbl)
				s.checkFlight(rt, tbl, "#sql "+tbl.SelectSQL())
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
