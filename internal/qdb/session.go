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

	mu       sync.Mutex
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

// call runs f, one C API call, under ctx on its own goroutine. It returns
// f's error if f finished in time; on a deadline it poisons the session,
// returns ErrCallTimeout, and the abandoned goroutine runs onAbandon (free
// any result, then dispose of the session) when fn returns. onAbandon must
// touch nothing the caller still reads.
func (s *Session) call(ctx context.Context, f func() error, onAbandon func()) error {
	s.mu.Lock()
	poisoned := s.poisoned
	s.mu.Unlock()
	if poisoned {
		return ErrCallTimeout
	}

	g := newGuard()
	g.onAbandon = func() { s.cluster.wedged.Add(1) }
	var fErr error
	go func() {
		fErr = f()
		if g.finish() {
			return
		}
		onAbandon()
		s.cluster.wedged.Add(-1)
	}()

	if g.wait(ctx) {
		return fErr
	}
	s.mu.Lock()
	s.poisoned = true
	s.mu.Unlock()
	return ErrCallTimeout
}

// query runs q and, on completion, hands its result to f on the calling
// goroutine, closing it when f returns. Execute may return
// a result next to an error, and Close is nil-safe, so the result is closed
// on every path. A query abandoned on the deadline closes its result on the
// goroutine that outlived the deadline; the caller, which never sees that
// result, does not touch it.
func (s *Session) query(ctx context.Context, q string, f func(*qdbapi.QueryResult) error) error {
	var result *qdbapi.QueryResult
	callErr := s.call(ctx,
		func() error {
			var err error
			result, err = s.session.Query(q).Execute()
			return err
		},
		func() {
			result.Close()
			s.disposeAbandoned()
		})
	if s.abandoned() {
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
