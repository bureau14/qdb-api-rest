// Package observe owns the operational surface of the server: the
// process logger and the metrics registry, how both travel through
// context, and the attribute vocabulary shared by every log line.
package observe

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"unicode/utf8"

	"github.com/bureau14/qdb-api-rest/internal/config"
)

// parseLevel maps the config vocabulary onto slog levels.
func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q", level)
}

// newHandler picks the output shape: JSON for machines (the default),
// text for humans at a terminal.
func newHandler(format string, opts *slog.HandlerOptions, w io.Writer) (slog.Handler, error) {
	switch format {
	case "json":
		return slog.NewJSONHandler(w, opts), nil
	case "text":
		return slog.NewTextHandler(w, opts), nil
	}
	return nil, fmt.Errorf("unknown log format %q", format)
}

// truncateStatement keeps at most max bytes of a statement and ends a
// truncated one in "...". A max of 0 keeps the statement whole.
func truncateStatement(s string, max int) string {
	if max == 0 || len(s) <= max {
		return s
	}
	// Back off to a rune start so a multi-byte character is not split. At
	// most three bytes, the longest a valid rune needs, because a body
	// need not be UTF-8.
	end := max
	for end > 0 && max-end < utf8.UTFMax-1 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "..."
}

// NewLogger builds the process logger. The caller places it in the root
// context with WithLogger, and nothing installs it as a global. It
// truncates KeyStatement at cfg.StatementMaxBytes.
func NewLogger(cfg config.Log, w io.Writer) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	// Truncating here keeps the limit out of the ctx and out of query.Run.
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == KeyStatement && a.Value.Kind() == slog.KindString {
				a.Value = slog.StringValue(truncateStatement(a.Value.String(), cfg.StatementMaxBytes))
			}
			return a
		},
	}
	handler, err := newHandler(cfg.Format, opts, w)
	if err != nil {
		return nil, err
	}
	return slog.New(handler), nil
}

// Attribute keys shared across packages, so one field name means one
// thing in every line.
const (
	KeyError     = "error"
	KeyRequestID = "request_id"
	// KeyUser is the caller's username as the token carries it, empty for
	// anonymous.
	KeyUser = "user"
	// KeySession is the REST session id claim.
	KeySession = "session"
	KeyPlane   = "plane"
	KeyEngine  = "engine"
	KeyOutcome = "outcome"
	KeyRows    = "rows"
	// KeyStatement is truncated by the process logger, and
	// KeyStatementBytes is the untruncated length.
	KeyStatement      = "statement"
	KeyStatementBytes = "statement_bytes"
	KeyTable          = "table"
	KeyStart          = "start"
	KeyEnd            = "end"
)

// Plane, Engine and Outcome are closed vocabularies. Named types keep a
// free string, or one vocabulary in place of another, from compiling.

// Plane names the listener a request came in on. Each plane's edge tags
// the request's logger with it.
type Plane string

const (
	// PlaneHTTP is the HTTP and the HTTPS listener, which serve one mux.
	PlaneHTTP   Plane = "http"
	PlaneFlight Plane = "flight"
)

// PlaneAttr is the plane under KeyPlane.
func PlaneAttr(p Plane) slog.Attr { return slog.String(KeyPlane, string(p)) }

// Engine names what ran a query. EngineBulkReader is the table reader,
// which runs no statement and gets its own word so its lines are not
// mistaken for native queries.
type Engine string

const (
	EngineNative     Engine = "native"
	EngineDuckDB     Engine = "duckdb"
	EngineBulkReader Engine = "bulk reader"
)

// EngineAttr is the engine under KeyEngine.
func EngineAttr(e Engine) slog.Attr { return slog.String(KeyEngine, string(e)) }

// Outcome is how a query ended. query.Logged says when each applies.
type Outcome string

const (
	OutcomeOK        Outcome = "ok"
	OutcomeError     Outcome = "error"
	OutcomeCut       Outcome = "cut"
	OutcomeAbandoned Outcome = "abandoned"
)

// OutcomeAttr is the outcome under KeyOutcome.
func OutcomeAttr(o Outcome) slog.Attr { return slog.String(KeyOutcome, string(o)) }

// Err renders the error under KeyError. A nil error yields an empty
// attribute, which handlers omit.
func Err(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	return slog.String(KeyError, err.Error())
}

// loggerKey is the context key for the logger. It is unexported so only
// WithLogger and Logger touch it.
type loggerKey struct{}

// WithLogger returns a context that carries the logger. Callees reach the
// logger through Logger, because it is explicit state of the call and
// never a global.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// Logger returns the logger the context carries. A context without one
// is a programming error, such as a context.Background() or TODO() in
// the middle of a call chain. Logger panics on it, so the call fails
// fast instead of logging somewhere nobody reads.
func Logger(ctx context.Context) *slog.Logger {
	l, ok := ctx.Value(loggerKey{}).(*slog.Logger)
	if !ok {
		panic("observe: no logger in context")
	}
	return l
}

// WithAttrs returns a child context whose logger adds the attributes to
// every record. The caller's context is untouched, so the attributes end
// where the child context goes out of scope.
func WithAttrs(ctx context.Context, attrs ...any) context.Context {
	return WithLogger(ctx, Logger(ctx).With(attrs...))
}
