package qdb

import (
	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// Session is one authenticated client session as this package sees it: the
// narrow wrapper around a qdb_handle_t and the only way code touches one;
// the binding's HandleType never leaves this package. Every method runs one
// C API operation and returns Go-owned memory, so the surface the server
// depends on is enumerable here and nothing outside it holds C memory. A
// Session is built per checkout: Call wraps the handle the user's pool
// leased, Probe the one it dialed itself. One goroutine uses a Session at
// a time.
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

// fetch runs q and returns its result copied into Go memory; the binding
// releases the C result before returning, so the set needs no Close. A
// statement that produces no result set (DDL) yields a nil set.
func (s *Session) fetch(q string) (*qdbapi.QueryResultSet, error) {
	return s.session.Query(q).Fetch()
}
