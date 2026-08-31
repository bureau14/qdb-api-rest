package qdb

import (
	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// Session is one authenticated client session as this package sees it: the
// narrow wrapper around a qdb_handle_t and the only way code touches one;
// the binding's HandleType never leaves this package. Every method runs one
// C API operation under the error classification Call applies and owns the
// release of any result, so the surface the server depends on is enumerable
// here. A Session is built per checkout: Call wraps the handle the user's
// pool leased, Probe the one it dialed itself. One goroutine uses a Session
// at a time.
type Session struct {
	session qdbapi.Session
}

func newSession(session qdbapi.Session) *Session {
	return &Session{session: session}
}

// closeAsync closes the handle on its own goroutine: qdb_close joins the
// handle's worker threads and can block for a long time. Only the probe's
// own session ends here; a pooled one ends through its lease, and the pool
// closes it the same way.
func (s *Session) closeAsync() {
	go func() { _ = s.session.Close() }()
}

// query runs q and hands its result to f, closing it when f returns.
// Execute may return a result next to an error and Close is nil-safe, so
// both paths close it.
func (s *Session) query(q string, f func(*qdbapi.QueryResult) error) error {
	result, err := s.session.Query(q).Execute()
	if err != nil {
		result.Close()
		return err
	}
	defer result.Close()
	return f(result)
}

// tagged returns the aliases carrying t.
func (s *Session) tagged(t string) ([]string, error) {
	return s.session.GetTagged(t)
}
