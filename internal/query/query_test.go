package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
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

// drain is a sink that pulls every batch.
func drain(b qdb.Batches) error {
	for _, err := range b {
		if err != nil {
			return err
		}
	}
	return nil
}

// errSink is the error of a sink that fails on purpose.
var errSink = errors.New("sink failed")

// captured is the statement line (nil if none) and the query line of one
// call.
type captured struct {
	statement, query map[string]any
}

// capture runs call on an empty log and returns its lines. It fails
// unless there is one query line, preceded by at most one statement line.
func capture(t *rapid.T, log *bytes.Buffer, call func()) captured {
	t.Helper()
	log.Reset()
	call()
	var c captured
	for _, raw := range bytes.Split(bytes.TrimSpace(log.Bytes()), []byte("\n")) {
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("log line %q: %v", raw, err)
		}
		switch line[slog.MessageKey] {
		case "query statement":
			if c.statement != nil || c.query != nil {
				t.Fatalf("a statement line after another line of the call: %s", log)
			}
			c.statement = line
		case "query":
			if c.query != nil {
				t.Fatalf("two query lines: %s", log)
			}
			c.query = line
		}
	}
	if c.query == nil {
		t.Fatalf("no query line: %s", log)
	}
	return c
}

// checkQuery fails unless the query line names the engine, the outcome
// and the rows.
func checkQuery(t *rapid.T, c captured, e observe.Engine, o observe.Outcome, rows int) {
	t.Helper()
	q := c.query
	if q[observe.KeyEngine] != string(e) || q[observe.KeyOutcome] != string(o) || q[observe.KeyRows] != float64(rows) {
		t.Fatalf("query line %v, want engine %s, outcome %s, rows %d", q, e, o, rows)
	}
}

// checkStatement fails unless the statement line carries the text and
// its length.
func checkStatement(t *rapid.T, c captured, text string) {
	t.Helper()
	st := c.statement
	if st == nil || st[observe.KeyStatement] != text || st[observe.KeyStatementBytes] != float64(len(text)) {
		t.Fatalf("statement line %v, want %q", st, text)
	}
}

// TestQueryLines: every call writes one query line with its engine,
// outcome and rows pulled. It is where countRows is proven, and the e2e
// round trip covers the call sites on both planes.
func TestQueryLines(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		// At debug, so the statement line is written.
		var log bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))
		ctx := qdb.WithCluster(observe.WithLogger(context.Background(), logger), f.Cluster)

		rapid.Check(t, func(rt *rapid.T) {
			tbl := table.Generate(rt)
			table.CreateAs(rt, f.Cluster, f.User, tbl)

			// 1. a native query reads the whole table
			c := capture(rt, &log, func() {
				if err := Run(ctx, f.User, tbl.Select(), drain); err != nil {
					rt.Fatalf("native: %v", err)
				}
			})
			checkQuery(rt, c, observe.EngineNative, observe.OutcomeOK, tbl.Rows())
			checkStatement(rt, c, tbl.Select())

			// 2. a #sql query, logged without the prefix
			c = capture(rt, &log, func() {
				if err := Run(ctx, f.User, "#sql "+tbl.SelectSQL(), drain); err != nil {
					rt.Fatalf("duckdb: %v", err)
				}
			})
			checkQuery(rt, c, observe.EngineDuckDB, observe.OutcomeOK, tbl.Rows())
			checkStatement(rt, c, tbl.SelectSQL())

			// 3. a table read: its attrs and no statement line
			start, end := tbl.Range()
			c = capture(rt, &log, func() {
				err := Logged(ctx, observe.EngineBulkReader, func(sink func(qdb.Batches) error) error {
					return f.Cluster.Read(ctx, f.User, tbl.Name, qdb.ReadOptions{Start: start, End: end}, sink)
				}, drain,
					slog.String(observe.KeyTable, tbl.Name),
					slog.String(observe.KeyStart, start.Format(time.RFC3339Nano)),
					slog.String(observe.KeyEnd, end.Format(time.RFC3339Nano)))
				if err != nil {
					rt.Fatalf("read: %v", err)
				}
			})
			checkQuery(rt, c, observe.EngineBulkReader, observe.OutcomeOK, tbl.Rows())
			if c.statement != nil || c.query[observe.KeyTable] != tbl.Name || c.query[observe.KeyStart] == nil || c.query[observe.KeyEnd] == nil {
				rt.Fatalf("table read lines %v, want no statement and the table and its range", c)
			}

			// 4. an unknown table
			c = capture(rt, &log, func() {
				if err := Run(ctx, f.User, "SELECT * FROM "+tbl.Name+"_missing", drain); err == nil {
					rt.Fatalf("a query of an unknown table succeeded")
				}
			})
			checkQuery(rt, c, observe.EngineNative, observe.OutcomeError, 0)
			if c.query[observe.KeyError] == nil {
				rt.Fatalf("refused query line %v, want the error", c.query)
			}

			// 5. a sink that fails after one batch counts that batch alone
			var pulled int
			c = capture(rt, &log, func() {
				err := Run(ctx, f.User, tbl.Select(), func(b qdb.Batches) error {
					for rec, err := range b {
						if err != nil {
							return err
						}
						pulled = int(rec.NumRows())
						return errSink
					}
					return errSink
				})
				if !errors.Is(err, errSink) {
					rt.Fatalf("cut: %v, want the sink's error", err)
				}
			})
			checkQuery(rt, c, observe.EngineNative, observe.OutcomeCut, pulled)

			// 6. the caller leaves
			left, leave := context.WithCancel(ctx)
			defer leave()
			c = capture(rt, &log, func() {
				err := Run(left, f.User, tbl.Select(), func(qdb.Batches) error {
					leave()
					return left.Err()
				})
				if !errors.Is(err, context.Canceled) {
					rt.Fatalf("abandoned: %v, want the ctx's error", err)
				}
			})
			checkQuery(rt, c, observe.EngineNative, observe.OutcomeAbandoned, 0)
		})
	})
}
