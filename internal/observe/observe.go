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
)

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
