// Package query runs one query string on the engine its text names. A
// string whose first non-whitespace bytes are #sql runs on DuckDB, and
// any other string runs on the native engine (ADR-0018). The HTTP query
// route calls it, and the Flight server will.
package query

import (
	"context"
	"strings"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// isSpace reports whether b is ASCII whitespace: space, tab, LF, CR, VT
// or FF.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

// split answers the text to run and whether DuckDB runs it. A #sql prefix
// after ASCII whitespace, in any case, followed by one ASCII whitespace
// byte, is DuckDB's, and text is what follows that byte. Any other q is
// native, and text is q unchanged, leading whitespace included.
func split(q string) (text string, duck bool) {
	// The rule has three conditions in an order, and each one that fails
	// sends q to the native engine unchanged. The test reads bytes and
	// decodes no UTF-8, so a query in any encoding splits the same way:
	//
	//  1. skip the leading whitespace;
	//  2. match #sql in any case;
	//  3. require one whitespace byte after it;
	//  4. strip the prefix and that one byte.

	// 1. skip the leading whitespace, because the rule reads the first
	//    non-whitespace bytes. unicode.IsSpace over runes was rejected,
	//    because it depends on decoding the text.
	i := 0
	for i < len(q) && isSpace(q[i]) {
		i++
	}
	// 2. match #sql in any case
	const prefix = "#sql"
	if len(q)-i < len(prefix) || !strings.EqualFold(q[i:i+len(prefix)], prefix) {
		return q, false
	}
	// 3. require one whitespace byte after it, so #sqlx and a bare #sql
	//    stay native and fail there
	i += len(prefix)
	if i >= len(q) || !isSpace(q[i]) {
		return q, false
	}
	// 4. strip the prefix and that one byte. Every byte after it stays, a
	//    second whitespace byte included, so the SQL reaches DuckDB as
	//    typed.
	return q[i+1:], true
}

// Run runs q as u and streams the result into sink. A query that starts
// with #sql runs on u's DuckDB instance without the prefix; any other
// query runs on the native engine as written. The cluster comes from
// ctx. Every error other than sink's own is returned before sink is
// called, so a caller can pick a status before writing. Sink's error is
// returned as is.
func Run(ctx context.Context, u qdb.User, q string, sink func(qdb.Batches) error) error {
	// Both engines hand sink the same lent Batches, so a caller reads a
	// result one way. The native engine gets one read retry, because the
	// query completes before sink runs and nothing has been sent. A DuckDB
	// query is never retried.
	text, duck := split(q)
	c := qdb.ClusterFrom(ctx)
	if duck {
		return c.Duck(ctx, u, text, sink)
	}
	return c.Query(ctx, u, text, sink, qdb.WithReadRetry())
}
