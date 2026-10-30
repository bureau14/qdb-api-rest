// Package observe owns the operational surface of the server: the
// process logger and the metrics registry, how both travel through
// context, and the attribute vocabulary shared by every log line.
package observe

import (
	"context"
	"fmt"
	"io"
	"log/slog"

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
func newHandler(format string, level slog.Level, w io.Writer) (slog.Handler, error) {
	opts := &slog.HandlerOptions{Level: level}
	switch format {
	case "json":
		return slog.NewJSONHandler(w, opts), nil
	case "console":
		return slog.NewTextHandler(w, opts), nil
	}
	return nil, fmt.Errorf("unknown log format %q", format)
}

// NewLogger builds the process logger. The caller places it in the root
// context with WithLogger, and nothing installs it as a global.
func NewLogger(cfg config.Log, w io.Writer) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	handler, err := newHandler(cfg.Format, level, w)
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
	// KeyStatement is a query's text.
	KeyStatement = "statement"
	// KeyStatementBytes is the length of the whole statement.
	KeyStatementBytes = "statement_bytes"
	KeyTable          = "table"
	KeyStart          = "start"
	KeyEnd            = "end"
)

// The plane, the engine and the outcome each take one of a closed set of
// values. Each is a named string type with typed constants and one
// attribute constructor, so a call site cannot write a free string under
// the key, and the compiler refuses one vocabulary where another is
// expected. Untyped string constants beside the keys were rejected,
// because a free string would still compile. A line struct was rejected
// too: slog reads no struct tags, and a LogValuer would nest the
// attributes under one group key and change every grep of the log.

// Plane names the listener a request came in on. The edge of each plane
// tags the request's logger with it, so every line of the request names
// its plane, the access line included. A plane argument on query.Run was
// rejected, because it would change a signature both planes call, and
// the table reader would need it as well.
type Plane string

const (
	// PlaneHTTP is the HTTP and the HTTPS listener, which serve one mux.
	PlaneHTTP   Plane = "http"
	PlaneFlight Plane = "flight"
)

// PlaneAttr is the plane under KeyPlane.
func PlaneAttr(p Plane) slog.Attr { return slog.String(KeyPlane, string(p)) }

// Engine names what ran a query. EngineBulkReader is the table reader,
// which reads a whole table through Cluster.Read and runs no statement.
// It has a word of its own because a table read is a data read like a
// query, and the line must tell the two apart. Logging a table read
// under EngineNative was rejected for that reason.
type Engine string

const (
	EngineNative     Engine = "native"
	EngineDuckDB     Engine = "duckdb"
	EngineBulkReader Engine = "bulk reader"
)

// EngineAttr is the engine under KeyEngine.
func EngineAttr(e Engine) slog.Attr { return slog.String(KeyEngine, string(e)) }

// Outcome is how a query ended, one value per case a call can tell apart
// without the plane's status, so a dashboard counts failures by kind
// without parsing messages. query.Logged says when each applies. A bare
// ok or error was rejected, because a cut stream and an engine's refusal
// would look alike.
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
