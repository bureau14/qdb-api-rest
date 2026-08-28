package qdb

import (
	"fmt"
	"sync"
	"time"
)

// breakerState is the circuit breaker's position.
type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// breaker fails fast for a cluster that stops answering: it opens after
// threshold consecutive retryable failures, half-opens after openFor to
// admit one probe, and closes again on a success. A call that the cluster
// answers -- even by rejecting the request -- counts as a success: the
// cluster is healthy, the caller was wrong.
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
// until the breaker next admits one. Closed: every call proceeds. Open:
// calls fail fast until openUntil; the first call to arrive after that
// moment flips the breaker to half-open and is admitted as the probe.
// Half-open: the probe is in flight and every other call fails fast; the
// hint is what is left of the open window, zero or less by then, so the
// caller may retry at once.
func (b *breaker) allow() (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		if remaining := b.openUntil.Sub(b.now()); remaining > 0 {
			return remaining, false
		}
		b.state = breakerHalfOpen
		return 0, true
	case breakerHalfOpen:
		return b.openUntil.Sub(b.now()), false
	default:
		return 0, true
	}
}

// recordSuccess closes the breaker whatever its state and forgets the
// failure streak: a call the cluster answered -- the half-open probe, or
// any call while closed -- proves the cluster is healthy.
func (b *breaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = breakerClosed
	b.failures = 0
}

// recordFailure counts one retryable failure. A failed half-open probe
// reopens the breaker for another openFor and leaves the streak alone.
// Otherwise the streak grows by one and, at threshold, opens the breaker
// for openFor; a failure that lands while already open (a call admitted
// before the breaker opened) only lengthens the streak, which the next
// success resets.
func (b *breaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == breakerHalfOpen {
		b.state = breakerOpen
		b.openUntil = b.now().Add(b.openFor)
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.state = breakerOpen
		b.openUntil = b.now().Add(b.openFor)
	}
}

// BreakerOpenError is returned by Call while the breaker is open; the HTTP
// layer maps it to 503 with a Retry-After of RetryAfter.
type BreakerOpenError struct {
	RetryAfter time.Duration
}

func (e *BreakerOpenError) Error() string {
	return fmt.Sprintf("qdb: circuit breaker open, retry after %s", e.RetryAfter)
}
