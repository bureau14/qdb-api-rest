// Package query runs one query string on the engine its text names. A
// string whose first non-whitespace bytes are #sql runs on DuckDB, and
// any other string runs on the native engine (ADR-0018). The HTTP query
// route calls Run, and the Flight server calls Run and Describe.
package query

import (
	"context"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// isSpace reports whether b is ASCII whitespace: space, tab, LF, CR, VT
// or FF.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

// split answers the text to run and whether DuckDB runs it. A #sql
// prefix in any case, after optional ASCII whitespace and followed by one
// ASCII whitespace byte, sends what follows that byte to DuckDB. Any
// other query is native and stays unchanged, leading whitespace included.
func split(q string) (text string, duck bool) {
	// The rule has three conditions, and the first that fails sends the
	// query to the native engine unchanged. The rule reads bytes and
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

// Run runs a query as the user and streams the result into the sink. A
// query that starts with #sql runs on the user's DuckDB instance without
// the prefix, and any other query runs on the native engine as written.
// The cluster comes from the context. Every error other than the sink's
// comes before the sink runs, so a caller can pick a status before
// writing. The sink's error is returned unchanged.
func Run(ctx context.Context, u qdb.User, q string, sink func(qdb.Batches) error) error {
	// Both engines hand the sink the same lent Batches, so a caller reads a
	// result one way. The native engine gets one read retry, because the
	// query completes before the sink runs and nothing has been sent. A
	// DuckDB query is never retried.
	text, duck := split(q)
	c := qdb.ClusterFrom(ctx)
	if duck {
		return c.Duck(ctx, u, text, sink)
	}
	return c.Query(ctx, u, text, sink, qdb.WithReadRetry())
}

// Describe answers the schema of a query's result without running it, on
// the engine Run would pick. The errors are those of qdb.Cluster.Describe
// and qdb.Cluster.DuckDescribe.
func Describe(ctx context.Context, u qdb.User, q string) (*arrow.Schema, error) {
	// The native describe takes the read retry, as Run's native query
	// does.
	text, duck := split(q)
	c := qdb.ClusterFrom(ctx)
	if duck {
		return c.DuckDescribe(ctx, u, text)
	}
	return c.Describe(ctx, u, text, qdb.WithReadRetry())
}
