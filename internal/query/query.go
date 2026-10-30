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

// countRows answers the batches unchanged and adds the rows of every
// batch the consumer pulled to n. A batch the consumer never pulled is
// not counted.
func countRows(b qdb.Batches, n *int64) qdb.Batches {
	return func(yield func(arrow.RecordBatch, error) bool) {
		for rec, err := range b {
			// An error element carries no batch and passes through
			// uncounted. A batch is counted before yield, because it is
			// lent and may be released once yield returns.
			if err == nil {
				*n += rec.NumRows()
			}
			if !yield(rec, err) {
				return
			}
		}
	}
}

// Logged runs call with a sink that wraps sink, then writes one info
// line with the engine, the duration, the rows the sink pulled, the
// outcome and the attrs. It returns call's error unchanged. The line
// inherits the plane, the request id and the user from the ctx's logger.
// The outcome is ok when call succeeded, abandoned when the ctx ended,
// cut when the sink failed, and error when call failed before the sink
// ran.
func Logged(ctx context.Context, e observe.Engine, call func(func(qdb.Batches) error) error, sink func(qdb.Batches) error, attrs ...slog.Attr) error {
	// The function wraps the sink to see the rows and the sink's own
	// error, runs the call, and then names the outcome from what it saw:
	//
	//  1. wrap the sink so it counts rows and keeps its own error apart
	//     from call's;
	//  2. run the call and time it;
	//  3. name the outcome;
	//  4. write the line at info, whatever the outcome.

	// 1. wrap the sink. Counting in each plane's sink was rejected, because
	//    each plane would copy the counter. The sink's error is kept apart,
	//    because a sink failure and an engine failure are different
	//    outcomes.
	var rows int64
	var sinkErr error
	wrapped := func(b qdb.Batches) error {
		sinkErr = sink(countRows(b, &rows))
		return sinkErr
	}

	// 2. run the call, timed in milliseconds as both access lines time a
	//    request
	start := time.Now()
	err := call(wrapped)
	elapsed := time.Since(start).Milliseconds()

	// 3. name the outcome. Every error other than the sink's arrives
	//    before the sink runs, so an error that is not the sink's is the
	//    engine's refusal. The ctx check comes before the sink check,
	//    because a caller who leaves mid-stream makes the sink fail too,
	//    and no answer reached a caller who left, whichever failed.
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

	// 4. write the line at info, with the error under KeyError, because
	//    the brief puts every query line at info. Warn for a failed query
	//    was rejected for that reason, and because the access line
	//    already carries the status.
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
// writing. The sink's error is returned unchanged. Every run writes one
// query line through Logged, preceded by a debug line with the
// statement.
func Run(ctx context.Context, u qdb.User, q string, sink func(qdb.Batches) error) error {
	// Both engines hand the sink the same lent Batches, so a caller reads a
	// result one way. The native engine gets one read retry, because the
	// query completes before the sink runs and nothing has been sent. A
	// DuckDB query is never retried. Logged names the engine split chose.
	text, duck := split(q)
	c := qdb.ClusterFrom(ctx)
	// The statement line is written before the run, because a crash in
	// DuckDB is a crash of the binary, and a line written after the run
	// would be lost with it. It carries the text the engine runs, without
	// the #sql prefix, because the query line's engine names DuckDB
	// already. The text goes whole with its length, and the process logger
	// truncates the text.
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
// nothing: the Flight server's DoGet runs the query and writes the line,
// and a line per describe would count a client's describe and run as two
// queries.
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
