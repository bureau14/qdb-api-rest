// Package query runs one query string on the engine its text names. A
// string whose first non-whitespace bytes are #sql runs on DuckDB, and
// any other string runs on the native engine (ADR-0018). The HTTP query
// route calls Run, and the Flight server calls Run and Describe.
package query

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/bureau14/qdb-api-rest/internal/observe"
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

// countRows passes the batches through and adds the rows of each batch
// the consumer pulls to n.
func countRows(b qdb.Batches, n *int64) qdb.Batches {
	return func(yield func(arrow.RecordBatch, error) bool) {
		for rec, err := range b {
			// Count before yield, because the batch is released after it.
			if err == nil {
				*n += rec.NumRows()
			}
			if !yield(rec, err) {
				return
			}
		}
	}
}

// Logged runs call with a wrapped sink and writes one info line with the
// engine, duration, rows pulled, outcome and attrs. It returns call's
// error unchanged. The outcome is ok on success, abandoned when the ctx
// ended, cut when the sink failed, and error otherwise.
func Logged(ctx context.Context, e observe.Engine, call func(func(qdb.Batches) error) error, sink func(qdb.Batches) error, attrs ...slog.Attr) error {
	// The sink's error is kept apart from call's to tell cut from error.
	var rows int64
	var sinkErr error
	wrapped := func(b qdb.Batches) error {
		sinkErr = sink(countRows(b, &rows))
		return sinkErr
	}

	start := time.Now()
	err := call(wrapped)
	elapsed := time.Since(start).Milliseconds()

	// The ctx goes first, because a caller who leaves makes the sink fail
	// too.
	var o observe.Outcome
	switch {
	case err == nil:
		o = observe.OutcomeOK
	case ctx.Err() != nil:
		o = observe.OutcomeAbandoned
	case sinkErr != nil:
		o = observe.OutcomeCut
	default:
		o = observe.OutcomeError
	}

	// Info whatever the outcome, as the brief fixes it. The access line
	// already carries the status.
	line := append([]slog.Attr{
		observe.EngineAttr(e),
		slog.Int64("duration_ms", elapsed),
		slog.Int64(observe.KeyRows, rows),
		observe.OutcomeAttr(o),
		observe.Err(err),
	}, attrs...)
	observe.Logger(ctx).LogAttrs(ctx, slog.LevelInfo, "query", line...)
	return err
}

// Run runs a query as the user and streams the result into the sink. A
// query that starts with #sql runs on the user's DuckDB instance without
// the prefix, and any other query runs on the native engine as written.
// The cluster comes from the context. Every error other than the sink's
// comes before the sink runs, so a caller can pick a status before
// writing. The sink's error is returned unchanged. Every run logs the
// statement at debug and one query line through Logged.
func Run(ctx context.Context, u qdb.User, q string, sink func(qdb.Batches) error) error {
	// Both engines hand the sink the same lent Batches, so a caller reads a
	// result one way. The native engine gets one read retry, because the
	// query completes before the sink runs and nothing has been sent. A
	// DuckDB query is never retried.
	text, duck := split(q)
	c := qdb.ClusterFrom(ctx)
	// Logged before the run, so a statement that crashes DuckDB, and the
	// binary with it, is in the log.
	observe.Logger(ctx).DebugContext(ctx, "query statement",
		slog.String(observe.KeyStatement, text), slog.Int(observe.KeyStatementBytes, len(text)))
	if duck {
		return Logged(ctx, observe.EngineDuckDB, func(s func(qdb.Batches) error) error {
			return c.Duck(ctx, u, text, s)
		}, sink)
	}
	return Logged(ctx, observe.EngineNative, func(s func(qdb.Batches) error) error {
		return c.Query(ctx, u, text, s, qdb.WithReadRetry())
	}, sink)
}

// Describe answers the schema of a query's result without running it, on
// the engine Run would pick. The errors are those of qdb.Cluster.Describe
// and qdb.Cluster.DuckDescribe. It writes no query line, because it runs
// nothing.
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
