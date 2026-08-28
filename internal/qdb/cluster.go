package qdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// User is the QuasarDB user a pool of sessions authenticates as: the pool
// key is the user, never a REST session or a token (ADR-0003). A user has
// one secret key, so every REST session of a user dials identically and
// shares the user's pool. Anonymous is the zero User.
type User struct {
	Username  string
	SecretKey string
}

// LogValue renders a user without its secret key.
func (u User) LogValue() slog.Value {
	if u.Username == "" {
		return slog.StringValue("(anonymous)")
	}
	return slog.StringValue(u.Username)
}

// budget is the process-wide ceiling on live sessions: a unit is taken
// before a user pool dials and released when the session is actually
// closed, so a session wedged in cgo keeps counting until its close returns.
type budget struct {
	tokens chan struct{}
}

func newBudget(max int) *budget {
	return &budget{tokens: make(chan struct{}, max)}
}

// acquire takes a unit, waiting for one or for ctx to end.
func (b *budget) acquire(ctx context.Context) error {
	select {
	case b.tokens <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *budget) release()   { <-b.tokens }
func (b *budget) inUse() int { return len(b.tokens) }
func (b *budget) max() int   { return cap(b.tokens) }

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

// allow reports whether a call may proceed, and if not, how long until the
// breaker next admits one.
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

func (b *breaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = breakerClosed
	b.failures = 0
}

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
	cfg     config.Cluster
	poolCfg config.Pool
	now     func() time.Time

	budget  *budget
	breaker *breaker
	wedged  atomic.Int64

	mu    sync.Mutex
	users map[string]*userPool
	stop  chan struct{}
	done  chan struct{}
}

// New builds the cluster from config. now defaults to time.Now; the tests
// pass a fake clock. The reaper goroutine starts here and stops on Close.
func New(cfg config.Config, now func() time.Time) *Cluster {
	if now == nil {
		now = time.Now
	}
	c := &Cluster{
		cfg:     cfg.Cluster,
		poolCfg: cfg.Pool,
		now:     now,
		budget:  newBudget(cfg.Pool.MaxSessions),
		breaker: newBreaker(cfg.Pool.Breaker.Failures, cfg.Pool.Breaker.OpenFor, now),
		users:   map[string]*userPool{},
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go c.reap()
	return c
}

// credentials identify one user to the cluster: username and secret key,
// or the user security file that carries both.
type credentials struct {
	username, secretKey, userSecurityFile string
}

func (u User) credentials() credentials {
	return credentials{username: u.Username, secretKey: u.SecretKey}
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
		hdl, dialErr = qdbapi.NewHandleFromOptions(c.handleOptions(u))
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
// user (outside the pool, the budget and the breaker), runs the readiness
// query, and closes the session on its own goroutine. The dial proves the
// cluster is reachable and the REST API's own user authenticates; the query
// proves the session serves one (ADR-0004).
func (c *Cluster) Probe(ctx context.Context, readinessQuery string) error {
	s, err := c.connect(ctx, c.ownCredentials(), false)
	if err != nil {
		return err
	}
	qerr := s.query(ctx, readinessQuery, func(*qdbapi.QueryResult) error { return nil })
	if !s.abandoned() {
		s.closeAsync()
	}
	return qerr
}

// reap closes idle sessions and evicts user pools that have held none for
// idle_timeout, on a fixed tick until Close.
func (c *Cluster) reap() {
	defer close(c.done)
	ticker := time.NewTicker(reapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.reapOnce()
		}
	}
}

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

// Stats is a snapshot of the whole cluster's session usage.
type Stats struct {
	BudgetInUse int
	BudgetMax   int
	Wedged      int
	Users       int
}

// Stats returns a snapshot.
func (c *Cluster) Stats() Stats {
	c.mu.Lock()
	users := len(c.users)
	c.mu.Unlock()
	return Stats{
		BudgetInUse: c.budget.inUse(),
		BudgetMax:   c.budget.max(),
		Wedged:      int(c.wedged.Load()),
		Users:       users,
	}
}

// Close stops the reaper and drains every user pool within ctx, returning
// ctx.Err() for whatever is still wedged. The process exits regardless.
func (c *Cluster) Close(ctx context.Context) error {
	close(c.stop)
	<-c.done
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

// context plumbing: the cluster travels next to the logger (ADR-0002's
// pattern), so handlers read it from the request context instead of
// taking it through a constructor.
type clusterKey struct{}

// WithCluster returns ctx carrying c.
func WithCluster(ctx context.Context, c *Cluster) context.Context {
	return context.WithValue(ctx, clusterKey{}, c)
}

// ClusterFrom returns the cluster carried by ctx, or nil.
func ClusterFrom(ctx context.Context) *Cluster {
	c, _ := ctx.Value(clusterKey{}).(*Cluster)
	return c
}
