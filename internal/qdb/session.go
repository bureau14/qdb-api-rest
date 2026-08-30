package qdb

import (
	"context"
	"errors"
	"sync"

	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/qdb/pool"
)

// ErrCallTimeout is returned when a C API call outlives its deadline. The
// call cannot be cancelled, so the session it ran on is poisoned: every
// later call on it returns this without touching cgo, and the abandoned
// goroutine closes the session when the C call finally returns.
var ErrCallTimeout = errors.New("qdb: call timed out")

// Session is one authenticated client session, the only way code touches
// a leased qdb_handle_t: the binding's HandleType never leaves this
// package. Every method runs its C call on its own goroutine under the
// call deadline (the failsafe in call), so the deadline and the error
// classification wrap every C call by construction.
// One goroutine uses a Session at a time; the pool never leases one twice.
type Session struct {
	cluster  *Cluster
	hdl      qdbapi.HandleType
	budgeted bool // holds a global budget unit, released on Close

	mu       sync.Mutex
	poisoned bool
	lease    *pool.Lease[*Session] // set while pooled; nil for the probe's own session
}

// newSession wraps a freshly connected session. budgeted sessions release a
// budget unit when closed; the probe's session does not hold one.
func newSession(c *Cluster, hdl qdbapi.HandleType, budgeted bool) *Session {
	return &Session{cluster: c, hdl: hdl, budgeted: budgeted}
}

// bind attaches the lease a pooled session is currently checked out under,
// so an abandoned call can discard it. A poisoned session is never leased
// again, so no stale lease is ever observed.
func (s *Session) bind(lease *pool.Lease[*Session]) {
	s.mu.Lock()
	s.lease = lease
	s.mu.Unlock()
}

// abandoned reports whether a call on this session timed out; the abandoned
// goroutine, not the caller, then owns the session's fate.
func (s *Session) abandoned() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.poisoned
}

// Close closes the underlying handle and releases the budget unit a
// budgeted session holds. qdb_close joins the handle's worker threads and
// can block for a long time; the pool always calls this on its own
// goroutine.
func (s *Session) Close() error {
	err := s.hdl.Close()
	if s.budgeted {
		s.cluster.budget.release()
	}
	return err
}

// closeAsync closes on its own goroutine; the probe uses it so a probe is
// never blocked by a slow close.
func (s *Session) closeAsync() {
	go func() { _ = s.Close() }()
}

// disposeAbandoned closes a session whose call was abandoned: a pooled
// session by discarding its lease (which the pool then closes), the probe's
// session by closing itself. It runs only after the C call has returned, so
// the close never races an in-flight call.
func (s *Session) disposeAbandoned() {
	s.mu.Lock()
	lease := s.lease
	s.mu.Unlock()
	if lease != nil {
		lease.Discard()
		return
	}
	s.closeAsync()
}

// call runs fn, one C API call, under ctx on its own goroutine. It returns
// fn's error if fn finished in time; on a deadline it poisons the session,
// returns ErrCallTimeout, and the abandoned goroutine runs onAbandon (free
// any result, then dispose of the session) when fn returns. onAbandon must
// touch nothing the caller still reads.
func (s *Session) call(ctx context.Context, fn func() error, onAbandon func()) error {
	s.mu.Lock()
	poisoned := s.poisoned
	s.mu.Unlock()
	if poisoned {
		return ErrCallTimeout
	}

	g := newGuard()
	g.onAbandon = func() { s.cluster.wedged.Add(1) }
	var fnErr error
	go func() {
		fnErr = fn()
		if g.finish() {
			return
		}
		onAbandon()
		s.cluster.wedged.Add(-1)
	}()

	if g.wait(ctx) {
		return fnErr
	}
	s.mu.Lock()
	s.poisoned = true
	s.mu.Unlock()
	return ErrCallTimeout
}

// query runs one native query and, on completion, hands its result to use
// on the calling goroutine, closing it when use returns. Execute may return
// a result next to an error, and Close is nil-safe, so the result is closed
// on every path. A query abandoned on the deadline closes its result on the
// goroutine that outlived the deadline; the caller, which never sees that
// result, does not touch it.
func (s *Session) query(ctx context.Context, text string, use func(*qdbapi.QueryResult) error) error {
	var result *qdbapi.QueryResult
	callErr := s.call(ctx,
		func() error {
			var err error
			result, err = s.hdl.Query(text).Execute()
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
	return use(result)
}

// tagged returns the aliases carrying tag; the binding frees its own C
// memory, so an abandoned call only disposes of the session.
func (s *Session) tagged(ctx context.Context, tag string) ([]string, error) {
	var aliases []string
	err := s.call(ctx,
		func() error {
			var e error
			aliases, e = s.hdl.GetTagged(tag)
			return e
		},
		s.disposeAbandoned)
	return aliases, err
}
