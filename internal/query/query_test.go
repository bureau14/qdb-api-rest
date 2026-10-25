package query

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// asciiSpaces are the bytes the prefix rule reads as whitespace.
var asciiSpaces = []byte{' ', '\t', '\n', '\r', '\v', '\f'}

// unicodeSpaces are spaces outside ASCII, which the prefix rule does not
// read as whitespace: no-break space, next line and em space.
var unicodeSpaces = []string{"\u00a0", "\u0085", "\u2003"}

// drawSpaces draws zero to three ASCII whitespace bytes.
func drawSpaces(t *rapid.T, label string) string {
	return string(rapid.SliceOfN(rapid.SampledFrom(asciiSpaces), 0, 3).Draw(t, label))
}

// drawPrefix draws #sql with each letter in either case.
func drawPrefix(t *rapid.T) string {
	var b strings.Builder
	for _, r := range "#sql" {
		if rapid.Bool().Draw(t, "upper") {
			r = []rune(strings.ToUpper(string(r)))[0]
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestSplit: for any leading ASCII whitespace, any case of #sql, one
// ASCII whitespace byte and any rest, split answers DuckDB and the rest. A
// query that misses one condition is native and unchanged. The test dials
// nothing, because split is pure.
func TestSplit(t *testing.T) {
	// The property draws one of several shapes, and each shape has its own
	// expected answer:
	//
	//  1. draw the leading whitespace from the six ASCII bytes;
	//  2. draw the case of each of the four prefix letters;
	//  3. draw a shape and build the query from it;
	//  4. compare split(q) with the shape's answer.
	rapid.Check(t, func(t *rapid.T) {
		// 1. the leading whitespace. Only ASCII bytes count as whitespace,
		//    because the rule reads bytes and decodes no UTF-8.
		lead := drawSpaces(t, "lead")
		// 2. the prefix in any case
		prefix := drawPrefix(t)
		rest := rapid.String().Draw(t, "rest")
		// 3. the shape. The rest may itself start with whitespace, which
		//    stays, because only the one byte after the prefix is removed.
		var q, want string
		var duck bool
		switch rapid.SampledFrom([]string{"duckdb", "glued", "bare", "unicode after", "unicode before", "other"}).Draw(t, "shape") {
		case "duckdb":
			q = lead + prefix + string(rapid.SampledFrom(asciiSpaces).Draw(t, "sep")) + rest
			want, duck = rest, true
		case "glued":
			// #sqlx and the like: a non-whitespace byte after the prefix
			q = lead + prefix + "x" + rest
			want = q
		case "bare":
			q = lead + prefix
			want = q
		case "unicode after":
			q = lead + prefix + rapid.SampledFrom(unicodeSpaces).Draw(t, "space") + rest
			want = q
		case "unicode before":
			q = rapid.SampledFrom(unicodeSpaces).Draw(t, "space") + prefix + " " + rest
			want = q
		case "other":
			// any text whose first non-whitespace byte is not '#'
			q = lead + rapid.StringMatching(`[^#\t\n\v\f\r ]`).Draw(t, "first") + rest
			want = q
		}
		// 4. compare
		text, got := split(q)
		if got != duck || text != want {
			t.Fatalf("split(%q) = %q, %v; want %q, %v", q, text, got, want, duck)
		}
	})
}
