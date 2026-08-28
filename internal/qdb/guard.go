package qdb

import (
	"context"
	"sync"
)

// guard runs one blocking cgo call on its own goroutine and races it
// against a deadline. It sits directly above the C call: Session.call
// wraps one C API operation in it, Cluster.connect wraps the dial. The C API has no cancellation path, so a call past
// its deadline cannot be stopped; the guard abandons its goroutine
// instead, and the goroutine, when the C call finally returns, owns the
// cleanup (freeing any result and closing the session). The caller never
// touches what an abandoned call produced.
type guard struct {
	mu        sync.Mutex
	done      chan struct{}
	completed bool
	abandoned bool
	onAbandon func() // runs once, under mu, the moment the caller gives up
}

func newGuard() *guard {
	return &guard{done: make(chan struct{})}
}

// finish is called by the work goroutine when its cgo call returns. It
// reports whether the caller is still waiting: true means the caller owns
// the outcome, false means the caller abandoned the work and the
// goroutine must clean up.
func (g *guard) finish() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.abandoned {
		return false
	}
	g.completed = true
	close(g.done)
	return true
}

// wait blocks until the work finishes or ctx ends. It reports whether the
// work completed. On a deadline it marks the work abandoned and runs
// onAbandon before any finish can observe the abandonment, so a counter
// bumped in onAbandon is always incremented before the goroutine's
// matching decrement.
func (g *guard) wait(ctx context.Context) bool {
	select {
	case <-g.done:
		return true
	case <-ctx.Done():
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.completed {
			return true
		}
		g.abandoned = true
		if g.onAbandon != nil {
			g.onAbandon()
		}
		return false
	}
}
