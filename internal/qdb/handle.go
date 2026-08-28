package qdb

import (
	"context"
	"errors"
	"sync"
	"unsafe"

	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/qdb/pool"
)

// ErrCallTimeout is returned when a C API call outlives its deadline. The
// call cannot be cancelled, so the handle it ran on is poisoned: every
// later call on it returns this without touching cgo, and the abandoned
// goroutine closes the handle when the C call finally returns.
var ErrCallTimeout = errors.New("qdb: call timed out")

// Handle is the only way code touches a leased qdb_handle_t: the binding's
// HandleType never leaves this package. Every method runs its C call on
// its own goroutine under the call deadline (the failsafe in call), so the
// deadline and the error classification wrap every C call by construction.
// One goroutine uses a Handle at a time; the pool never leases one twice.
type Handle struct {
	cluster  *Cluster
	hdl      qdbapi.HandleType
	budgeted bool // holds a global budget unit, released on Close

	mu       sync.Mutex
	poisoned bool
	lease    *pool.Lease[*Handle] // set while pooled; nil for the probe's own handle
}

// newHandle wraps a freshly connected handle. budgeted handles release a
// budget unit when closed; the probe's handle does not hold one.
func newHandle(c *Cluster, hdl qdbapi.HandleType, budgeted bool) *Handle {
	return &Handle{cluster: c, hdl: hdl, budgeted: budgeted}
}

// bind attaches the lease a pooled handle is currently checked out under,
// so an abandoned call can discard it. A poisoned handle is never leased
// again, so no stale lease is ever observed.
func (h *Handle) bind(lease *pool.Lease[*Handle]) {
	h.mu.Lock()
	h.lease = lease
	h.mu.Unlock()
}

// abandoned reports whether a call on this handle timed out; the abandoned
// goroutine, not the caller, then owns the handle's fate.
func (h *Handle) abandoned() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.poisoned
}

// Close closes the underlying handle and releases the budget unit a
// budgeted handle holds. Closing a handle can block for a long time; the
// pool always calls this on its own goroutine.
func (h *Handle) Close() error {
	err := h.hdl.Close()
	if h.budgeted {
		h.cluster.budget.release()
	}
	return err
}

// closeAsync closes on its own goroutine; the probe uses it so a probe is
// never blocked by a slow close.
func (h *Handle) closeAsync() {
	go func() { _ = h.Close() }()
}

// disposeAbandoned closes a handle whose call was abandoned: a pooled
// handle by discarding its lease (which the pool then closes), the probe's
// handle by closing itself. It runs only after the C call has returned, so
// the close never races an in-flight call.
func (h *Handle) disposeAbandoned() {
	h.mu.Lock()
	lease := h.lease
	h.mu.Unlock()
	if lease != nil {
		lease.Discard()
		return
	}
	h.closeAsync()
}

// call runs fn, one C API call, under ctx on its own goroutine. It returns
// fn's error if fn finished in time; on a deadline it poisons the handle,
// returns ErrCallTimeout, and the abandoned goroutine runs onAbandon (free
// any result, then dispose of the handle) when fn returns. onAbandon must
// touch nothing the caller still reads.
func (h *Handle) call(ctx context.Context, fn func() error, onAbandon func()) error {
	h.mu.Lock()
	poisoned := h.poisoned
	h.mu.Unlock()
	if poisoned {
		return ErrCallTimeout
	}

	g := newGuard()
	g.onAbandon = func() { h.cluster.wedged.Add(1) }
	var fnErr error
	go func() {
		fnErr = fn()
		if g.finish() {
			return
		}
		onAbandon()
		h.cluster.wedged.Add(-1)
	}()

	if g.wait(ctx) {
		return fnErr
	}
	h.mu.Lock()
	h.poisoned = true
	h.mu.Unlock()
	return ErrCallTimeout
}

// query runs one native query and, on completion, hands its result to use
// on the calling goroutine, freeing it when use returns. A query abandoned
// on the deadline frees its result on the goroutine that outlived the
// deadline; the caller, which never sees that result, does not touch it.
func (h *Handle) query(ctx context.Context, text string, use func(*qdbapi.QueryResult) error) error {
	var result *qdbapi.QueryResult
	callErr := h.call(ctx,
		func() error {
			var err error
			result, err = h.hdl.Query(text).Execute()
			return err
		},
		func() {
			if result != nil {
				h.hdl.Release(unsafe.Pointer(result))
			}
			h.disposeAbandoned()
		})
	if h.abandoned() {
		return callErr
	}
	if result != nil {
		defer h.hdl.Release(unsafe.Pointer(result))
	}
	if callErr != nil {
		return callErr
	}
	return use(result)
}

// tagged returns the aliases carrying tag; the binding frees its own C
// memory, so an abandoned call only disposes of the handle.
func (h *Handle) tagged(ctx context.Context, tag string) ([]string, error) {
	var aliases []string
	err := h.call(ctx,
		func() error {
			var e error
			aliases, e = h.hdl.GetTagged(tag)
			return e
		},
		h.disposeAbandoned)
	return aliases, err
}
