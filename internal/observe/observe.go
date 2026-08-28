// Package observe owns the operational surface of the server: the
// process logger, how it travels through context, and the attribute
// vocabulary shared by every log line.
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
// context with WithLogger; nothing installs it as a global.
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
	KeyCluster   = "cluster"
	KeyUsername  = "username"
	KeyHandle    = "handle"
)

// Err renders err under KeyError; a nil err yields an empty attr, which
// handlers omit.
func Err(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	return slog.String(KeyError, err.Error())
}

// loggerKey is the context key for the logger; unexported so only
// WithLogger and Logger touch it.
type loggerKey struct{}

// WithLogger returns ctx carrying l. Callees reach it through
// Logger(ctx): the logger is explicit state of the call, never a global.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// Logger returns the logger carried by ctx. A ctx without one is a
// programming error (a context.Background() or TODO() mid-call-chain)
// and panics: fail fast rather than log somewhere nobody reads.
func Logger(ctx context.Context) *slog.Logger {
	l, ok := ctx.Value(loggerKey{}).(*slog.Logger)
	if !ok {
		panic("observe: no logger in context")
	}
	return l
}

// WithAttrs returns a child ctx whose logger carries attrs on every
// record. Scope is lexical: the caller's ctx is untouched, so the attrs
// end where the child ctx goes out of scope.
func WithAttrs(ctx context.Context, attrs ...any) context.Context {
	return WithLogger(ctx, Logger(ctx).With(attrs...))
}
