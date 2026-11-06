package v1

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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
