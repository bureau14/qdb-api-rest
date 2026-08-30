package qdb

import (
	"context"
	"log/slog"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// bindingLogger adapts the process logger to qdb-api-go's package-level
// Logger interface, which carries no context: the binding logs through a
// global, so the adapter holds the logger it is given (ADR-0002 allows a
// stored logger exactly where no context is available). The binding's
// Info lines are its own housekeeping, noise at a gateway's request
// volume, so Info maps to Debug; its Panic is not fatal here and maps to
// Error.
type bindingLogger struct{ log *slog.Logger }

// InstallLogger routes qdb-api-go's logs into the process logger. It must
// run before the first dial. The binding logs with attribute pairs, so
// the adapter forwards them straight through.
func InstallLogger(log *slog.Logger) {
	qdbapi.SetLogger(&bindingLogger{log: log})
}

func (l *bindingLogger) at(level slog.Level, msg string, args ...any) {
	l.log.Log(context.Background(), level, msg, args...) //nolint:sloglint // bridges qdb-api-go's own (dynamic) log messages
}

func (l *bindingLogger) Detailed(msg string, args ...any) { l.at(slog.LevelDebug, msg, args...) }
func (l *bindingLogger) Debug(msg string, args ...any)    { l.at(slog.LevelDebug, msg, args...) }
func (l *bindingLogger) Info(msg string, args ...any)     { l.at(slog.LevelDebug, msg, args...) }
func (l *bindingLogger) Warn(msg string, args ...any)     { l.at(slog.LevelWarn, msg, args...) }
func (l *bindingLogger) Error(msg string, args ...any)    { l.at(slog.LevelError, msg, args...) }
func (l *bindingLogger) Panic(msg string, args ...any)    { l.at(slog.LevelError, msg, args...) }

// With returns an adapter over a logger that carries args on every line.
func (l *bindingLogger) With(args ...any) qdbapi.Logger {
	return &bindingLogger{log: l.log.With(args...)}
}

// compile-time check that the adapter is a complete qdbapi.Logger.
var _ qdbapi.Logger = (*bindingLogger)(nil)
