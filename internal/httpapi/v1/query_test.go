package v1

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// queryPaths are the two spellings of the v1 query.
var queryPaths = []string{"/api/v1/query", "/api/query"}

// tokenForm is one way a request presents its token.
type tokenForm int

const (
	bearerHeader tokenForm = iota // Authorization: Bearer <token>
	bareHeader                    // Authorization: <token>
	queryToken                    // ?token=<token>
)

// mintAccess mints an access token for the user from the context's
// keychain.
func mintAccess(t table.T, ctx context.Context, u qdb.User) string {
	t.Helper()
	token, err := auth.TokensFrom(ctx).Mint(auth.Claims{Username: u.Username, SecretKey: u.SecretKey, Typ: "access", SessionID: "sid-1", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return token
}

// postQuery posts the body to path with the token in the form, and
// answers the recorded response.
func postQuery(ctx context.Context, h http.Handler, path, body string, form tokenForm, token string) *httptest.ResponseRecorder {
	if form == queryToken {
		path += "?token=" + url.QueryEscape(token)
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body))
	switch form {
	case bearerHeader:
		req.Header.Set("Authorization", "Bearer "+token)
	case bareHeader:
		req.Header.Set("Authorization", token)
	}
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	return resp
}

// queryBody is the old server's {"query"} body for the query text.
func queryBody(q string) string {
	b, _ := json.Marshal(v1Query{Query: q})
	return string(b)
}

// oldBody is the old server's query answer as a client decodes it, with
// numbers kept as their text.
type oldBody struct {
	Tables []struct {
		Name    *string `json:"name"`
		Columns []struct {
			Data []any  `json:"data"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"columns"`
	} `json:"tables"`
}

// readOld decodes an old body.
func readOld(t table.T, resp *httptest.ResponseRecorder) oldBody {
	t.Helper()
	var b oldBody
	dec := json.NewDecoder(bytes.NewReader(resp.Body.Bytes()))
	dec.UseNumber()
	if err := dec.Decode(&b); err != nil {
		t.Fatalf("body %q: %v", resp.Body.String(), err)
	}
	return b
}

// oldWord is the old server's word for a QuasarDB column type.
func oldWord(c qdbapi.TsColumnType) string {
	switch c {
	case qdbapi.TsColumnInt64:
		return "int64"
	case qdbapi.TsColumnDouble:
		return "double"
	case qdbapi.TsColumnBlob:
		return "blob"
	case qdbapi.TsColumnTimestamp:
		return "timestamp"
	default:
		// A symbol arrives as utf8, as a string does.
		return "string"
	}
}

// appendOldCell appends one old cell to the builder of its column's type.
func appendOldCell(t table.T, b array.Builder, v any) {
	t.Helper()
	if v == nil {
		b.AppendNull()
		return
	}
	var err error
	switch b := b.(type) {
	case *array.Int64Builder:
		var n int64
		n, err = v.(json.Number).Int64()
		b.Append(n)
	case *array.Float64Builder:
		var f float64
		f, err = v.(json.Number).Float64()
		b.Append(f)
	case *array.StringBuilder:
		b.Append(v.(string))
	case *array.BinaryBuilder:
		var raw []byte
		raw, err = base64.StdEncoding.DecodeString(v.(string))
		b.Append(raw)
	case *array.TimestampBuilder:
		// encoding/json writes a UTC time with a Z and without trailing
		// zeros, so every timestamp text ends in Z.
		s := v.(string)
		if !strings.HasSuffix(s, "Z") {
			t.Fatalf("timestamp %q is not in UTC", s)
		}
		var ts time.Time
		ts, err = time.Parse(time.RFC3339Nano, s)
		b.Append(arrow.Timestamp(ts.UnixNano()))
	}
	if err != nil {
		t.Fatalf("cell %v: %v", v, err)
	}
}

// readOldBody turns the old body's one table back into a batch of the
// table's schema, column by column, so table.Check can compare it.
func readOldBody(t table.T, tbl table.Table, b oldBody) arrow.RecordBatch {
	t.Helper()
	schema := table.Schema(tbl)
	cols := make([]arrow.Array, schema.NumFields())
	for i, f := range schema.Fields() {
		bld := array.NewBuilder(memory.DefaultAllocator, f.Type)
		for _, v := range b.Tables[0].Columns[i].Data {
			appendOldCell(t, bld, v)
		}
		cols[i] = bld.NewArray()
		bld.Release()
	}
	rec := array.NewRecordBatch(schema, cols, int64(tbl.Rows()))
	for _, c := range cols {
		c.Release()
	}
	t.Cleanup(rec.Release)
	return rec
}

// wantWords is each column's old word: none for a column without a value,
// else the word of its type. $timestamp is never null.
func wantWords(tbl table.Table) []string {
	words := []string{"timestamp"}
	for i, c := range tbl.Columns {
		w := "none"
		if col := tbl.Batch.Column(i + 1); col.NullN() < col.Len() {
			w = oldWord(c.Type)
		}
		words = append(words, w)
	}
	return words
}

// TestQuery: on both clusters, at both spellings and under each way the
// old server took a token, a generated table queried through v1 answers
// the old body: no table for no rows, else one table without a name, the
// columns in order with their old words, none for an all-null column, and
// cells that read back as the rows written.
func TestQuery(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		ctx, h := newServer(t, f.Cluster)
		token := mintAccess(t, ctx, f.User)
		rapid.Check(t, func(rt *rapid.T) {
			tbl := table.Generate(rt)
			table.CreateAs(rt, f.Cluster, f.User, tbl)
			path := rapid.SampledFrom(queryPaths).Draw(rt, "path")
			// A Bearer header, a bare header and ?token= are the three forms
			// the old server took.
			form := rapid.SampledFrom([]tokenForm{bearerHeader, bareHeader, queryToken}).Draw(rt, "token form")
			resp := postQuery(ctx, h, path, queryBody(tbl.Select()), form, token)
			if resp.Code != http.StatusOK {
				rt.Fatalf("%s: status %d: %s", path, resp.Code, resp.Body.String())
			}
			if tbl.Rows() == 0 {
				if resp.Body.String() != "{\"tables\":[]}\n" {
					rt.Fatalf("%s: no rows answered %s", path, resp.Body.String())
				}
				return
			}
			// The prefix pins the key order and the dropped empty name.
			if !strings.HasPrefix(resp.Body.String(), `{"tables":[{"columns":[{"data":`) {
				rt.Fatalf("%s: body %.80s", path, resp.Body.String())
			}
			b := readOld(rt, resp)
			if len(b.Tables) != 1 || b.Tables[0].Name != nil {
				rt.Fatalf("%s: tables %+v", path, b.Tables)
			}
			fields := table.Schema(tbl).Fields()
			words := wantWords(tbl)
			if len(b.Tables[0].Columns) != len(fields) {
				rt.Fatalf("%s: %d columns, want %d", path, len(b.Tables[0].Columns), len(fields))
			}
			for i, c := range b.Tables[0].Columns {
				if c.Name != fields[i].Name || c.Type != words[i] {
					rt.Fatalf("%s: column %d is %s %s, want %s %s", path, i, c.Name, c.Type, fields[i].Name, words[i])
				}
			}
			table.Check(rt, tbl, readOldBody(rt, tbl, b))
		})
	})
}

// errorShape checks the status and that the body is a JSON object holding
// a message, the code where the shape carries one, and no other key.
func errorShape(t *testing.T, name string, resp *httptest.ResponseRecorder, status, code int) {
	t.Helper()
	var m map[string]any
	if resp.Code != status || json.Unmarshal(resp.Body.Bytes(), &m) != nil {
		t.Fatalf("%s: status %d, want %d: %s", name, resp.Code, status, resp.Body.String())
	}
	if msg, _ := m["message"].(string); msg == "" {
		t.Fatalf("%s: no message: %s", name, resp.Body.String())
	}
	// A 401 and the old server's request errors carry the code, and a
	// failed query carries the message alone.
	want := 1
	if code != 0 {
		want = 2
		if m["code"] != float64(code) {
			t.Fatalf("%s: code %v, want %d", name, m["code"], code)
		}
	}
	if len(m) != want {
		t.Fatalf("%s: keys %v", name, m)
	}
}

// TestQueryErrors: each row answers its status and its body shape, at both
// spellings.
func TestQueryErrors(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		ctx, h := newServer(t, f.Cluster)
		token := mintAccess(t, ctx, f.User)
		rows := []struct {
			name, header, param, body string
			status, code              int
		}{
			{"no token", "", "", queryBody("SELECT 1"), http.StatusUnauthorized, http.StatusUnauthorized},
			{"bad token", "Bearer nope", "", queryBody("SELECT 1"), http.StatusUnauthorized, http.StatusUnauthorized},
			// The old server tried ?token= after a header that failed.
			{"bad header, good ?token=", "Bearer nope", token, queryBody("show tables"), http.StatusOK, 0},
			{"empty body", "Bearer " + token, "", "", http.StatusUnprocessableEntity, 602},
			{"bare string", "Bearer " + token, "", `"show tables"`, http.StatusBadRequest, http.StatusBadRequest},
			{"undecodable", "Bearer " + token, "", `{"query":`, http.StatusBadRequest, http.StatusBadRequest},
			{"invalid query", "Bearer " + token, "", queryBody("NOT A QUERY"), http.StatusBadRequest, 0},
			{"unknown table", "Bearer " + token, "", queryBody("SELECT * FROM qdbtest_v1_none"), http.StatusBadRequest, 0},
			// A list is a type no cell writer knows.
			{"#sql unsupported type", "Bearer " + token, "", queryBody("#sql SELECT [1]"), http.StatusInternalServerError, 0},
		}
		for _, path := range queryPaths {
			for _, row := range rows {
				p := path
				if row.param != "" {
					p += "?token=" + url.QueryEscape(row.param)
				}
				req := httptest.NewRequestWithContext(ctx, http.MethodPost, p, strings.NewReader(row.body))
				if row.header != "" {
					req.Header.Set("Authorization", row.header)
				}
				resp := httptest.NewRecorder()
				h.ServeHTTP(resp, req)
				name := path + " " + row.name
				if row.status == http.StatusOK {
					if resp.Code != http.StatusOK {
						t.Fatalf("%s: status %d: %s", name, resp.Code, resp.Body.String())
					}
					continue
				}
				errorShape(t, name, resp, row.status, row.code)
			}
		}
	})
}

// TestQueryUnreachable: a cluster that does not answer is 500 {"message"}
// at both spellings.
func TestQueryUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1" // the connection is refused at once
	c := qdb.New(cfg, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	ctx, h := newServer(t, c)
	// Minted from the keychain, because a login on this cluster fails.
	token := mintAccess(t, ctx, qdb.User{})
	for _, path := range queryPaths {
		errorShape(t, path, postQuery(ctx, h, path, queryBody("SELECT 1"), bearerHeader, token), http.StatusInternalServerError, 0)
	}
}

// TestQuerySQL: on both clusters, one #sql row of every DuckDB scalar
// answers each column under its word, the old word for the old five types
// and the Arrow name for the rest, and each cell as the v2 JSON writes it.
func TestQuerySQL(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		ctx, h := newServer(t, f.Cluster)
		token := mintAccess(t, ctx, f.User)
		// The cells past the old five are copied from TestDuckDBScalars,
		// because the cell is the v2 JSON's. The old five are encoding/json's,
		// and a column without a value is none.
		want := []struct{ expr, word, cell string }{
			{"1::BIGINT", "int64", "1"},
			{"1.5::DOUBLE", "double", "1.5"},
			{"'a'", "string", `"a"`},
			{"'a'::BLOB", "blob", `"YQ=="`},
			{"TIMESTAMP '2026-06-11 00:00:00.5'", "timestamp", `"2026-06-11T00:00:00.5Z"`},
			{"1::TINYINT", "int8", "1"},
			{"1::SMALLINT", "int16", "1"},
			{"1", "int32", "1"},
			{"1::UTINYINT", "uint8", "1"},
			{"1::USMALLINT", "uint16", "1"},
			{"1::UINTEGER", "uint32", "1"},
			{"1::UBIGINT", "uint64", "1"},
			{"1.5::FLOAT", "float32", "1.5"},
			{"sum(1)", "decimal", "1"},
			{"1.50::DECIMAL(5,2)", "decimal", "1.50"},
			{"true", "bool", "true"},
			{"DATE '2026-06-11'", "date32", `"2026-06-11"`},
			{"TIME '24:00:00'", "time64", `"24:00:00.000000000"`},
			{"NULL", "none", "null"},
		}
		exprs := make([]string, len(want))
		for i, w := range want {
			exprs[i] = fmt.Sprintf("%s AS c%d", w.expr, i)
		}
		for _, path := range queryPaths {
			resp := postQuery(ctx, h, path, queryBody("#sql SELECT "+strings.Join(exprs, ", ")), bearerHeader, token)
			var body struct {
				Tables []struct {
					Columns []struct {
						Data []json.RawMessage
						Type string
					}
				}
			}
			if resp.Code != http.StatusOK || json.Unmarshal(resp.Body.Bytes(), &body) != nil {
				t.Fatalf("%s: status %d: %s", path, resp.Code, resp.Body.String())
			}
			if len(body.Tables) != 1 || len(body.Tables[0].Columns) != len(want) {
				t.Fatalf("%s: want one table of %d columns: %s", path, len(want), resp.Body.String())
			}
			for i, c := range body.Tables[0].Columns {
				if c.Type != want[i].word || len(c.Data) != 1 || string(c.Data[0]) != want[i].cell {
					t.Errorf("%s %s: type %q data %s, want type %q data [%s]", path, want[i].expr, c.Type, c.Data, want[i].word, want[i].cell)
				}
			}
		}
	})
}

// TestFind: on both clusters, a query that begins with find answers one
// table per entry the expression matches, with columns null, and no table
// for no match. Leading whitespace and FIND reach the native engine, which
// answers 400.
func TestFind(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		ctx, h := newServer(t, f.Cluster)
		token := mintAccess(t, ctx, f.User)
		tbl := table.Table{Name: "qdbtest_v1_find", Columns: []table.Column{{Name: "c0", Type: qdbapi.TsColumnInt64}}}
		table.CreateAs(t, f.Cluster, f.User, tbl)
		tag := "qdbtest_v1_tag"
		table.RemoveTagOnCleanupAs(t, f.Cluster, f.User, tag)
		// v1 has no tag route, so the test attaches the tag through v2.
		attach := httptest.NewRequestWithContext(ctx, http.MethodPut, "/api/v2/tables/"+tbl.Name+"/tags/"+tag, nil)
		attach.Header.Set("Authorization", "Bearer "+token)
		attached := httptest.NewRecorder()
		h.ServeHTTP(attached, attach)
		if attached.Code != http.StatusNoContent {
			t.Fatalf("attach: status %d: %s", attached.Code, attached.Body.String())
		}
		found := `{"tables":[{"name":"` + tbl.Name + `","columns":null}]}` + "\n"
		for _, path := range queryPaths {
			for _, row := range []struct{ query, body string }{
				{"find(tag='" + tag + "')", found},
				{"find(tag='qdbtest_v1_tag_none')", "{\"tables\":[]}\n"},
			} {
				resp := postQuery(ctx, h, path, queryBody(row.query), bearerHeader, token)
				if resp.Code != http.StatusOK || resp.Body.String() != row.body {
					t.Fatalf("%s %s: status %d: %s", path, row.query, resp.Code, resp.Body.String())
				}
			}
			for _, q := range []string{" find(tag='" + tag + "')", "FIND(tag='" + tag + "')"} {
				errorShape(t, path+" "+q, postQuery(ctx, h, path, queryBody(q), bearerHeader, token), http.StatusBadRequest, 0)
			}
		}
	})
}
