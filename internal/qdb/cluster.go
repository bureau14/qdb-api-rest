package qdb

import (
	"context"
	"errors"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/qdb/pool"
)

// reapInterval is how often idle sessions are closed and empty user pools
// evicted. It is a constant, well under any sensible idle_timeout, not a
// config knob: a dialed session is always used at least once more, so the
// tick only has to sit below idle_timeout.
const reapInterval = 10 * time.Second

// userPool is one user's bounded set of sessions plus when it last held
// none, for LRU eviction.
type userPool struct {
	pool       *pool.Pool[*Session]
	emptySince time.Time // zero while the pool holds a session
}

// Cluster binds this process to one configured cluster: the dial options,
// the global budget, the breaker, and the per-user session pools. It is the
// only door to a session (Call) and answers readiness on its own dial
// (Probe). Nothing dials at startup; every session is dialed on demand.
type Cluster struct {
	cfg            config.Cluster
	poolCfg        config.Pool
	readinessQuery string // status.readiness_query, run by Probe
	now            func() time.Time

	// budget caps the live sessions of the whole process at max_sessions:
	// a unit is held from before a dial until the session's close returns.
	budget *budget
	// breaker is the per-cluster circuit breaker Call gates on; dials and
	// calls feed it, the readiness probe never does (ADR-0004).
	breaker *breaker
	// wedged counts dials and calls abandoned on their deadline whose C
	// call has not returned yet. Each still holds its budget unit; the
	// abandoned goroutine decrements wedged once its close has completed.
	wedged atomic.Int64

	mu    sync.Mutex           // guards users
	users map[string]*userPool // one session pool per user, keyed by username
	// reaperStop ends the reaper goroutine and reaperDone closes once it
	// has ended; Close uses both so no reap pass races the drain.
	reaperStop chan struct{}
	reaperDone chan struct{}
}

// New builds the cluster from config. now defaults to time.Now; the tests
// pass a fake clock. The reaper goroutine starts here and stops on Close.
func New(cfg config.Config, now func() time.Time) *Cluster {
	if now == nil {
		now = time.Now
	}
	c := &Cluster{
		cfg:            cfg.Cluster,
		poolCfg:        cfg.Pool,
		readinessQuery: cfg.Status.ReadinessQuery,
		now:            now,
		budget:         newBudget(cfg.Pool.MaxSessions),
		breaker:        newBreaker(cfg.Pool.Breaker.Failures, cfg.Pool.Breaker.OpenFor, now),
		users:          map[string]*userPool{},
		reaperStop:     make(chan struct{}),
		reaperDone:     make(chan struct{}),
	}
	go c.reap()
	return c
}

// ownCredentials are the REST API's own user, from config.
func (c *Cluster) ownCredentials() credentials {
	return credentials{username: c.cfg.Username, secretKey: c.cfg.SecretKey, userSecurityFile: c.cfg.UserSecurityFile}
}

// compressionOf and encryptionOf map the config vocabulary onto the
// binding's enums; validation has already rejected anything else.
func compressionOf(s string) qdbapi.Compression {
	if s == "balanced" {
		return qdbapi.CompBalanced
	}
	return qdbapi.CompNone
}

func encryptionOf(s string) qdbapi.Encryption {
	if s == "aes" {
		return qdbapi.EncryptAES
	}
	return qdbapi.EncryptNone
}

// handleOptions turns the cluster config plus one user's credentials into
// a dial specification. A zero per-session knob is left at the C API
// default (the binding applies a knob only when non-zero).
func (c *Cluster) handleOptions(u credentials) *qdbapi.HandleOptions {
	o := qdbapi.NewHandleOptions().
		WithClusterUri(c.cfg.URI).
		WithCompression(compressionOf(c.cfg.Compression)).
		WithEncryption(encryptionOf(c.cfg.Encryption)).
		WithTimeout(c.cfg.Timeout)
	if c.cfg.PublicKey != "" {
		o = o.WithClusterPublicKey(c.cfg.PublicKey)
	}
	if c.cfg.PublicKeyFile != "" {
		o = o.WithClusterPublicKeyFile(c.cfg.PublicKeyFile)
	}
	switch {
	case u.userSecurityFile != "":
		o = o.WithUserSecurityFile(u.userSecurityFile)
	case u.username != "":
		o = o.WithUserName(u.username).WithUserSecret(u.secretKey)
	}
	if c.cfg.MaxInBufferSize > 0 {
		o = o.WithClientMaxInBufSize(uint(c.cfg.MaxInBufferSize))
	}
	if c.cfg.Parallelism > 0 {
		o = o.WithClientMaxParallelism(c.cfg.Parallelism)
	}
	if c.cfg.ConnectionsPerAddress > 0 {
		o = o.WithConnectionsPerAddress(c.cfg.ConnectionsPerAddress)
	}
	return o
}

// connect dials one session for u under the call deadline, through the same
// abandon-on-deadline failsafe as a call: a dial to a black-holed address
// blocks in cgo past its deadline, so the goroutine is abandoned and
// closes the session itself when connect finally returns. budgeted dials
// take a budget unit first and release it when the session closes (or, when
// abandoned or failed, as soon as the dial resolves).
func (c *Cluster) connect(ctx context.Context, u credentials, budgeted bool) (*Session, error) {
	if budgeted {
		if err := c.budget.acquire(ctx); err != nil {
			return nil, err
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, c.poolCfg.CallTimeout)
	defer cancel()

	g := newGuard()
	g.onAbandon = func() { c.wedged.Add(1) }
	var hdl qdbapi.HandleType
	var dialErr error
	go func() {
		hdl, dialErr = qdbapi.NewSessionFactory(c.handleOptions(u)).NewSession()
		if g.finish() {
			return
		}
		if dialErr == nil {
			_ = hdl.Close()
		}
		if budgeted {
			c.budget.release()
		}
		c.wedged.Add(-1)
	}()

	if !g.wait(callCtx) {
		return nil, ErrCallTimeout
	}
	if dialErr != nil {
		if budgeted {
			c.budget.release()
		}
		return nil, dialErr
	}
	return newSession(c, hdl, budgeted), nil
}

// dial is the pool's dial function for one user: a budgeted connect.
func (c *Cluster) dial(u credentials) pool.Dial[*Session] {
	return func(ctx context.Context) (*Session, error) {
		return c.connect(ctx, u, true)
	}
}

// poolFor finds or creates the user's pool; it never replaces one that
// exists, so in-flight requests are never raced.
func (c *Cluster) poolFor(u User) *pool.Pool[*Session] {
	c.mu.Lock()
	defer c.mu.Unlock()
	up, ok := c.users[u.Username]
	if !ok {
		up = &userPool{pool: pool.New(c.dial(u.credentials()), pool.Options{
			Max:         c.poolCfg.PerUserMax,
			IdleTimeout: c.poolCfg.IdleTimeout,
			MaxLifetime: c.poolCfg.MaxLifetime,
			Now:         c.now,
		})}
		c.users[u.Username] = up
	}
	up.emptySince = time.Time{}
	return up.pool
}

// callConfig carries per-call options.
type callConfig struct {
	retry bool
}

// CallOption tunes one Call.
type CallOption func(*callConfig)

// WithReadRetry permits one transparent retry on a fresh session after a
// retryable failure. Only idempotent reads may ask for it; ingestion never
// does (a batch push offers no way to prove non-application).
func WithReadRetry() CallOption {
	return func(cc *callConfig) { cc.retry = true }
}

// Call runs op against a session authenticated as u. It gates on the
// breaker, checks a session out of u's pool (dialing one on demand), runs
// op, and decides the session's fate: a success or a fatal error (the
// cluster answered) releases the session and closes the breaker; a
// retryable error or a deadline discards it and feeds the breaker. A
// deadline leaves the session to its abandoned goroutine; Call never
// touches it. WithReadRetry runs op once more on a fresh session after a
// retryable failure.
func (c *Cluster) Call(ctx context.Context, u User, op func(*Session) error, opts ...CallOption) error {
	var cc callConfig
	for _, opt := range opts {
		opt(&cc)
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		remaining, ok := c.breaker.allow()
		if !ok {
			return &BreakerOpenError{RetryAfter: remaining}
		}
		lease, aerr := c.poolFor(u).Acquire(ctx)
		if aerr != nil {
			if !errors.Is(aerr, context.Canceled) && !errors.Is(aerr, context.DeadlineExceeded) {
				c.breaker.recordFailure()
			}
			return aerr
		}
		s := lease.Conn()
		s.bind(lease)
		err = op(s)
		switch {
		case s.abandoned():
			c.breaker.recordFailure() // the abandoned goroutine owns the lease
		case err == nil:
			lease.Release()
			c.breaker.recordSuccess()
			return nil
		case qdbapi.IsRetryable(err):
			lease.Discard()
			c.breaker.recordFailure()
		default: // fatal: the cluster answered and rejected the request
			lease.Release()
			c.breaker.recordSuccess()
			return err
		}
		if !cc.retry {
			return err
		}
		cc.retry = false
	}
	return err
}

// Query runs a native query as u and hands each result to use.
func (c *Cluster) Query(ctx context.Context, u User, text string, use func(*qdbapi.QueryResult) error, opts ...CallOption) error {
	return c.Call(ctx, u, func(s *Session) error { return s.query(ctx, text, use) }, opts...)
}

// Tagged returns the aliases carrying tag, read as u.
func (c *Cluster) Tagged(ctx context.Context, u User, tag string) ([]string, error) {
	var aliases []string
	err := c.Call(ctx, u, func(s *Session) error {
		var e error
		aliases, e = s.tagged(ctx, tag)
		return e
	}, WithReadRetry())
	return aliases, err
}

// Probe answers readiness. It dials a fresh session as the REST API's own
// user (outside the pool, the budget and the breaker), runs
// status.readiness_query, and closes the session on its own goroutine. The
// dial proves the cluster is reachable and the REST API's own user
// authenticates; the query proves the session serves one (ADR-0004).
func (c *Cluster) Probe(ctx context.Context) error {
	s, err := c.connect(ctx, c.ownCredentials(), false)
	if err != nil {
		return err
	}
	qerr := s.query(ctx, c.readinessQuery, func(*qdbapi.QueryResult) error { return nil })
	if !s.abandoned() {
		s.closeAsync()
	}
	return qerr
}

// reap closes idle sessions and evicts user pools that have held none for
// idle_timeout, on a fixed tick until Close.
func (c *Cluster) reap() {
	defer close(c.reaperDone)
	ticker := time.NewTicker(reapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.reaperStop:
			return
		case <-ticker.C:
			c.reapOnce()
		}
	}
}

// reapOnce closes the idle sessions of every user pool, then evicts the
// pools that have held nothing for idle_timeout: the first pass that
// finds a pool empty stamps emptySince, a later pass past idle_timeout
// deletes the pool, and a pool that holds anything again loses the stamp.
func (c *Cluster) reapOnce() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, up := range c.users {
		up.pool.Reap()
		s := up.pool.Stats()
		if s.InUse+s.Idle+s.Closing+s.Dialing > 0 {
			up.emptySince = time.Time{}
			continue
		}
		if up.emptySince.IsZero() {
			up.emptySince = now
			continue
		}
		if now.Sub(up.emptySince) >= c.poolCfg.IdleTimeout {
			delete(c.users, name)
		}
	}
}

// Reap runs one eviction pass; the tests drive it against a fake clock
// instead of waiting for the ticker.
func (c *Cluster) Reap() { c.reapOnce() }

// ClusterStats is a snapshot of the whole cluster's session usage.
type ClusterStats struct {
	BudgetInUse int
	BudgetMax   int
	Wedged      int
	Users       int
}

// Stats returns a snapshot.
func (c *Cluster) Stats() ClusterStats {
	c.mu.Lock()
	users := len(c.users)
	c.mu.Unlock()
	return ClusterStats{
		BudgetInUse: c.budget.inUse(),
		BudgetMax:   c.budget.max(),
		Wedged:      int(c.wedged.Load()),
		Users:       users,
	}
}

// Close first stops the reaper (and waits for its goroutine to end, so no
// eviction pass runs during the drain), then takes every user pool out of
// the map and drains each within ctx, returning ctx.Err() for whatever is
// still wedged. The process exits regardless.
func (c *Cluster) Close(ctx context.Context) error {
	close(c.reaperStop)
	<-c.reaperDone
	c.mu.Lock()
	pools := maps.Values(c.users)
	c.users = map[string]*userPool{}
	c.mu.Unlock()
	var err error
	for up := range pools {
		if e := up.pool.Close(ctx); e != nil && err == nil {
			err = e
		}
	}
	return err
}
