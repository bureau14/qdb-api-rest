package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// capture returns a JSON logger and a decoder for the last line it wrote.
func capture() (*slog.Logger, func(*testing.T) map[string]any) {
	var buf bytes.Buffer
	last := func(t *testing.T) map[string]any {
		t.Helper()
		lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
		var record map[string]any
		if err := json.Unmarshal(lines[len(lines)-1], &record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	return slog.New(slog.NewJSONHandler(&buf, nil)), last
}

func TestWithAttrsScopes(t *testing.T) {
	logger, last := capture()
	parent := WithLogger(context.Background(), logger)
	child := WithAttrs(parent, "a", 1)
	grandchild := WithAttrs(child, "b", 2)

	Logger(grandchild).InfoContext(grandchild, "x")
	if rec := last(t); rec["a"] == nil || rec["b"] == nil {
		t.Errorf("grandchild lost attrs: %v", rec)
	}
	Logger(child).InfoContext(child, "x")
	if rec := last(t); rec["a"] == nil || rec["b"] != nil {
		t.Errorf("child scope wrong: %v", rec)
	}
	Logger(parent).InfoContext(parent, "x")
	if rec := last(t); rec["a"] != nil || rec["b"] != nil {
		t.Errorf("parent scope leaked: %v", rec)
	}
}

func TestLoggerMissingPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("want panic for a context without a logger")
		}
	}()
	Logger(context.Background())
}
