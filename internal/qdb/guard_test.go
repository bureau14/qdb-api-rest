package qdb

import (
	"context"
	"testing"
)

// TestGuardOwnership pins who owns the outcome of a guarded call: the
// caller when the work finishes before the deadline, the work goroutine
// when the deadline fires first. The C API is not involved; the stall a
// deadline races against cannot be mocked there and is not this
// package's to test.
func TestGuardOwnership(t *testing.T) {
	t.Run("finish before the deadline", func(t *testing.T) {
		g := newGuard()
		abandoned := 0
		g.onAbandon = func() { abandoned++ }
		if !g.finish() {
			t.Fatal("finish before any wait must report the caller as owner")
		}
		if !g.wait(context.Background()) {
			t.Fatal("wait after finish must report completion")
		}
		if abandoned != 0 {
			t.Fatalf("onAbandon ran %d times on a completed call", abandoned)
		}
	})
	t.Run("deadline before finish", func(t *testing.T) {
		g := newGuard()
		abandoned := 0
		g.onAbandon = func() { abandoned++ }
		expired, cancel := context.WithCancel(context.Background())
		cancel()
		if g.wait(expired) {
			t.Fatal("wait on an expired ctx must report abandonment")
		}
		if abandoned != 1 {
			t.Fatalf("onAbandon ran %d times, want exactly once", abandoned)
		}
		if g.finish() {
			t.Fatal("finish after abandonment must leave the outcome to the work goroutine")
		}
	})
}
