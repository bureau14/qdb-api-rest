// Package observe wires the operational surface of the server: logging
// now; metrics and build info arrive in later milestones.
package observe

import (
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

// NewLogger builds the process logger; the caller owns installing it as
// the slog default.
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
