package qdb

import (
	"context"
	"sync"
)

// guard is the ownership handshake between a caller waiting under a
// deadline and the goroutine that Session.call or Cluster.connect spawns
// for one blocking cgo call; the guard itself runs nothing and holds no
// timer. Exactly one side wins. The work finishing first hands the caller
// the outcome; the deadline firing first marks the work abandoned, and
// the goroutine -- told so by finish returning false -- owns the cleanup
// (freeing any result, closing the session) once the C call returns. The
// split exists because the C API can neither cancel a call in flight nor
// survive a close from another thread: the call must be left to finish,
// and only whoever saw it return may close the session.
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
		// The caller gave up while the call ran; clean-up is ours.
		return false
	}
	// The caller is still waiting: publish completion and wake it.
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
			// finish won the race to the lock; take the outcome anyway.
			return true
		}
		// Abandon: from here finish returns false and the goroutine
		// cleans up after itself.
		g.abandoned = true
		if g.onAbandon != nil {
			g.onAbandon()
		}
		return false
	}
}
