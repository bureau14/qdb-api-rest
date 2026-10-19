// Package query runs one query string on the engine its text names. A
// string whose first non-whitespace bytes are #sql runs on DuckDB, and
// any other string runs on the native engine (ADR-0018). The HTTP query
// route calls it, and the Flight server will.
package query

import (
	"context"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/bureau14/qdb-api-rest/internal/model"
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

// one is rec as Batches of one step. The caller owns rec and releases it
// after the step.
func one(rec arrow.RecordBatch) qdb.Batches {
	return func(yield func(arrow.RecordBatch, error) bool) {
		yield(rec, nil)
	}
}

// Run runs q as u on the engine its text names and hands the result to
// sink. The cluster is the one ctx carries. A #sql query runs on u's
// DuckDB instance with the prefix stripped, and a native query runs
// unchanged. Every error that is not sink's comes before sink runs, so a
// caller decides its answer before it writes. The error of sink is
// returned unchanged. A native statement without a result set is one step
// of a batch with no fields and no rows.
func Run(ctx context.Context, u qdb.User, q string, sink func(qdb.Batches) error) error {
	// Two engines with different retry and ownership rules meet in one
	// result shape, the lent Batches, so a caller has one way to read a
	// result:
	//
	//  1. split q into its text and its engine;
	//  2. a DuckDB query is Cluster.Duck, which streams its own lent
	//     batches;
	//  3. a native query is Cluster.Query with one read retry;
	//  4. the native batch is owned here and released after sink returns;
	//  5. hand sink the batch as one step.

	// 1. split q into its text and its engine
	text, duck := split(q)
	c := qdb.ClusterFrom(ctx)
	// 2. a DuckDB query is never retried and streams its own lent batches,
	//    which Cluster.Duck releases
	if duck {
		return c.Duck(ctx, u, text, sink)
	}
	// 3. a native query with one read retry, safe because the query runs
	//    before sink and nothing has been sent
	rec, err := c.Query(ctx, u, text, qdb.WithReadRetry())
	if err != nil {
		return err
	}
	// 4. the batch is released after sink returns rather than inside the
	//    step, so a sink that never pulls the step leaks nothing. A nil
	//    batch, a statement without a result set, becomes a batch of an
	//    empty schema, because Arrow's EncodeStream reads the first step's
	//    schema and Batches promises at least one step.
	if rec == nil {
		rec = model.EmptyBatch(arrow.NewSchema(nil, nil))
	}
	defer rec.Release()
	// 5. hand sink the batch as one step
	return sink(one(rec))
}
