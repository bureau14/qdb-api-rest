package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"
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

// TestTruncateStatement: for any statement and any max, the truncation
// is the statement itself when max is 0 or the statement fits, and
// otherwise a valid UTF-8 prefix of at most max bytes followed by "...".
// The test dials nothing, because the truncation is pure.
func TestTruncateStatement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// rapid.String draws ASCII and multi-byte runes both, so a cut can
		// land inside a rune of any width.
		s := rapid.String().Draw(t, "statement")
		max := rapid.IntRange(0, len(s)+4).Draw(t, "max")
		got := truncateStatement(s, max)

		// A max of 0 disables the truncation, and a statement that fits
		// stays whole.
		if max == 0 || len(s) <= max {
			if got != s {
				t.Fatalf("truncateStatement(%q, %d) = %q, want it unchanged", s, max, got)
			}
			return
		}
		// Otherwise the answer is a prefix of the statement and "...".
		prefix, ok := strings.CutSuffix(got, "...")
		if !ok || !strings.HasPrefix(s, prefix) {
			t.Fatalf("truncateStatement(%q, %d) = %q, want a prefix and ...", s, max, got)
		}
		// The prefix fits max and is valid UTF-8, and the back-off to a
		// rune start dropped less than one rune.
		if len(prefix) > max || !utf8.ValidString(prefix) || len(prefix) <= max-utf8.UTFMax {
			t.Fatalf("truncateStatement(%q, %d) = %q, a prefix of %d bytes", s, max, got, len(prefix))
		}
	})
}
