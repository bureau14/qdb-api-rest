// This file holds the sql endpoint's scalar test. The good path in every
// media type is the round trip (roundtrip_test.go), and the error rows
// live in errors_test.go.
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
)

// TestSQLScalars posts one row of every DuckDB scalar to /api/v2/sql and
// checks that the JSON result names each column by its Arrow type and
// writes each cell in the encoders' text. It proves that DuckDB answers
// the types the encoders write, in the words the encoders write them.
func TestSQLScalars(t *testing.T) {
	s := newServer(t)

	// 1. The query casts one literal to each type. NULL stands alone,
	// because DuckDB types a bare NULL as int32.
	want := []struct{ expr, word, cell string }{
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
		{"NULL", "int32", "null"},
	}
	exprs := make([]string, len(want))
	for i, w := range want {
		exprs[i] = fmt.Sprintf("%s AS c%d", w.expr, i)
	}
	resp := s.post("/api/v2/sql", "SELECT "+strings.Join(exprs, ", "), map[string]string{"Authorization": "Bearer " + s.token})

	// 2. The answer is 200 in JSON.
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	if ct := resp.Header().Get("Content-Type"); ct != encoding.JSONContentType {
		t.Fatalf("Content-Type = %q", ct)
	}

	// 3. The sql endpoint streams, so the body is an array of results, here
	// one result of one row.
	var body []struct {
		Columns []struct {
			Name string
			Type string
			Data []json.RawMessage
		}
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: %v", resp.Body.String(), err)
	}
	if len(body) != 1 || len(body[0].Columns) != len(want) {
		t.Fatalf("want one result of %d columns: %s", len(want), resp.Body.String())
	}

	// 4. Each column carries its Arrow word and its cell in the encoders'
	// text.
	for i, c := range body[0].Columns {
		if c.Type != want[i].word || len(c.Data) != 1 || string(c.Data[0]) != want[i].cell {
			t.Errorf("%s: type %q data %s, want type %q data [%s]", want[i].expr, c.Type, c.Data, want[i].word, want[i].cell)
		}
	}
}
