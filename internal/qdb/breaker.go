package qdb

import (
	"fmt"
	"sync"
	"time"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// breakerState is the circuit breaker's position.
type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// breaker fails fast for a cluster that stops answering. It opens after
// threshold consecutive failures that are evidence the cluster is
// unreachable or too busy to answer. It half-opens after openFor to
// admit one probe, and it closes again on a success. A call that the
// cluster answers counts as a success, and a rejected request is an
// answer: the cluster is healthy and the caller was wrong.
type breaker struct {
	mu        sync.Mutex
	state     breakerState
	failures  int
	openUntil time.Time
	threshold int
	openFor   time.Duration
	now       func() time.Time
}

func newBreaker(threshold int, openFor time.Duration, now func() time.Time) *breaker {
	return &breaker{state: breakerClosed, threshold: threshold, openFor: openFor, now: now}
}

// allow reports whether a call may proceed and, when it may not, how long
// until the breaker next admits one.
func (b *breaker) allow() (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		if remaining := b.openUntil.Sub(b.now()); remaining > 0 {
			// The breaker is still open, so the call fails fast and the
			// caller hears when to come back.
			return remaining, false
		}
		// The window has passed, so this caller becomes the probe.
		b.state = breakerHalfOpen
		return 0, true
	case breakerHalfOpen:
		// A probe is in flight, so everyone else keeps failing fast. The
		// hint is what is left of the window, which is zero or less by now.
		return b.openUntil.Sub(b.now()), false
	default:
		// The breaker is closed, so every call proceeds.
		return 0, true
	}
}

// recordSuccess closes the breaker whatever its state and forgets the
// failure streak, because a call the cluster answered proves the cluster
// is healthy. That call is the half-open probe, or any call while closed.
func (b *breaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = breakerClosed
	b.failures = 0
}

// recordFailure counts one cluster-unavailable failure.
func (b *breaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == breakerHalfOpen {
		// The probe failed, so the breaker reopens for another window. The
		// streak is already past the threshold and stays as it is.
		b.state = breakerOpen
		b.openUntil = b.now().Add(b.openFor)
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		// The streak reached the threshold, so the breaker opens for the
		// window. A failure that lands while the breaker is already open
		// only lengthens the streak.
		b.state = breakerOpen
		b.openUntil = b.now().Add(b.openFor)
	}
}

// BreakerOpenError is returned by Call while the breaker is open. The HTTP
// layer maps it to 503 with a Retry-After of RetryAfter.
type BreakerOpenError struct {
	RetryAfter time.Duration
}

func (e *BreakerOpenError) Error() string {
	return fmt.Sprintf("qdb: circuit breaker open, retry after %s", e.RetryAfter)
}

// IsClusterUnavailable reports whether err means the cluster was
// unreachable, timed out or was too busy to answer. This is the binding's
// own classification, the same one that feeds the breaker. The HTTP
// layer maps it to 503.
func IsClusterUnavailable(err error) bool {
	return qdbapi.IsClusterUnavailable(err)
}
