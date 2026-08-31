package qdb

import (
	"context"
	"fmt"
	"sync"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// ErrCallTimeout is returned when a C API call outlives its deadline. It
// wraps the binding's ErrTimeout, so errors.Is(err, qdbapi.ErrTimeout)
// holds and IsRetryable classifies it like any timeout the C API reports
// itself; the sentinel still tells the Go-side deadline apart. The call
// cannot be cancelled, so the session it ran on is poisoned: every later
// call on it returns this without touching cgo, and the abandoned
// goroutine closes the session when the C call finally returns.
var ErrCallTimeout = fmt.Errorf("qdb: call timed out: %w", qdbapi.ErrTimeout)

// Session is one authenticated client session as this package sees it: the
// narrow wrapper around a qdb_handle_t and the only way code touches one;
// the binding's HandleType never leaves this package. Every method runs its
// C call on its own goroutine under the call deadline (the failsafe in
// call), so the deadline and the error classification wrap every C call by
// construction. A Session is built per checkout: Call wraps the handle the
// user's pool leased, Probe the one it dialed itself. One goroutine uses a
// Session at a time; the pool never leases a handle twice.
type Session struct {
	cluster *Cluster
	session qdbapi.Session
	lease   *qdbapi.Lease // the lease the handle is checked out under; nil for the probe's own session

	mu sync.Mutex
	// poisoned is set by call, exactly once, when the deadline fires
	// while a C call is still blocked in cgo. From then on every method
	// returns ErrCallTimeout without touching cgo, Call never releases
	// the session back to its pool, and the abandoned goroutine -- the
	// only owner left -- disposes of it when the C call finally returns.
	poisoned bool
}

func newSession(c *Cluster, session qdbapi.Session, lease *qdbapi.Lease) *Session {
	return &Session{cluster: c, session: session, lease: lease}
}

// abandoned reports whether a call on this session timed out; the abandoned
// goroutine, not the caller, then owns the session's fate.
func (s *Session) abandoned() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.poisoned
}

// closeAsync closes the handle on its own goroutine: qdb_close joins the
// handle's worker threads and can block for a long time. Only the probe's
// own session ends here; a pooled one ends through its lease, and the pool
// closes it the same way.
func (s *Session) closeAsync() {
	go func() { _ = s.session.Close() }()
}

// disposeAbandoned closes a session whose call was abandoned: a pooled
// session by discarding its lease (which the pool then closes), the probe's
// session by closing itself. It runs only after the C call has returned, so
// the close never races an in-flight call.
func (s *Session) disposeAbandoned() {
	if s.lease != nil {
		s.lease.Discard()
		return
	}
	s.closeAsync()
}

// call runs f, one C API call, under ctx. Strategy: f runs on its own
// goroutine so the deadline can be honored even though the C API has no
// cancellation path, and the guard decides who owns the outcome. Work
// first: the caller gets f's error. Deadline first: the caller returns
// ErrCallTimeout at once, the session is poisoned, and the goroutine --
// now the sole owner -- runs onAbandon (free any result, then dispose of
// the session) when the C call finally returns. onAbandon must touch
// nothing the caller still reads.
func (s *Session) call(ctx context.Context, f func() error, onAbandon func()) error {
	// A poisoned session has an abandoned call possibly still blocked in
	// cgo; touching the handle again would race it.
	s.mu.Lock()
	poisoned := s.poisoned
	s.mu.Unlock()
	if poisoned {
		return ErrCallTimeout
	}

	// wedged is bumped in onAbandon -- under the guard's lock, before the
	// goroutine can observe the abandonment -- and dropped by the
	// goroutine once its cleanup is done, so the counter never dips.
	g := newGuard()
	g.onAbandon = func() { s.cluster.wedged.Add(1) }
	var fErr error
	go func() {
		fErr = f()
		if g.finish() {
			// The caller is still waiting and owns the outcome.
			return
		}
		// Abandoned: the caller returned long ago; clean up here.
		onAbandon()
		s.cluster.wedged.Add(-1)
	}()

	if g.wait(ctx) {
		return fErr
	}
	// Deadline first: poison the session and leave it to the goroutine.
	s.mu.Lock()
	s.poisoned = true
	s.mu.Unlock()
	return ErrCallTimeout
}

// query runs q and, on completion, hands its result to f on the calling
// goroutine, closing it when f returns.
//
// The result cannot be deferred closed at the top: on a deadline its
// ownership moves to the abandoned goroutine, which closes it when the C
// call returns, and a caller-side Close would race that one (Close is
// idempotent but not goroutine-safe). So each owner closes the result on
// its own path -- and since Execute may return a result next to an error
// and Close is nil-safe, each does so unconditionally.
func (s *Session) query(ctx context.Context, q string, f func(*qdbapi.QueryResult) error) error {
	var result *qdbapi.QueryResult
	callErr := s.call(ctx,
		func() error {
			var err error
			result, err = s.session.Query(q).Execute()
			return err
		},
		func() {
			// Abandoned: this goroutine owns the result now.
			result.Close()
			s.disposeAbandoned()
		})
	if s.abandoned() {
		// The abandoned goroutine owns result; never touch it here.
		return callErr
	}
	defer result.Close()
	if callErr != nil {
		return callErr
	}
	return f(result)
}

// tagged returns the aliases carrying t; the binding frees its own C
// memory, so an abandoned call only disposes of the session.
func (s *Session) tagged(ctx context.Context, t string) ([]string, error) {
	var aliases []string
	err := s.call(ctx,
		func() error {
			var e error
			aliases, e = s.session.GetTagged(t)
			return e
		},
		s.disposeAbandoned)
	return aliases, err
}
