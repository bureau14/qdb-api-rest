package qdb

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/model"
	"github.com/bureau14/qdb-api-rest/internal/olap"
)

// evictInterval is how often user pools that hold no session are evicted.
const evictInterval = 10 * time.Second

// userPool is one user's bounded set of sessions, the user's DuckDB
// instance once a DuckDB call has opened it, the connections open on that
// instance, and the time at which the pool was last found holding none of
// these, for LRU eviction.
type userPool struct {
	pool       *qdbapi.SessionPool
	emptySince time.Time // zero while the pool holds a session or a connection
	// duckMu serialises the open of the instance, which dials the cluster,
	// so that two first calls of one user open one instance and other
	// users are not held.
	duckMu sync.Mutex
	duck   *olap.Instance // nil until the user's first DuckDB call; guarded by duckMu
	duckIn int            // DuckDB calls on duck; guarded by Cluster.mu
}

// Cluster binds this process to one configured cluster: the dial options,
// the global budget, the breaker, and the per-user session pools. Call is
// the only way to a session, and Probe answers readiness on a dial of its
// own. Nothing dials at startup. Every session is dialed on demand.
type Cluster struct {
	cfg            config.Cluster
	poolCfg        config.Pool
	duckCfg        config.DuckDB
	readinessQuery string // status.readiness_query, run by Probe
	now            func() time.Time

	// budget caps the live sessions of the whole process at max_sessions.
	// A unit is held from before a dial until the session's close returns.
	budget *budget
	// breaker is the per-cluster circuit breaker that Call gates on. Dials
	// and calls that find the cluster unavailable feed it. The readiness
	// probe does not feed it.
	breaker *breaker

	mu    sync.Mutex           // protects users, instances and every duckIn
	users map[string]*userPool // one session pool per user, keyed by username
	// instances counts the live DuckDB instances against
	// duckdb.max_instances. A unit is held from before an open until the
	// instance's close returns.
	instances int
	// evictStop ends the eviction goroutine, and evictDone closes once it
	// has ended. Close uses both, so no eviction pass races the drain.
	evictStop chan struct{}
	evictDone chan struct{}
}

// New builds the cluster from config. now defaults to time.Now, and the
// tests pass a fake clock. The eviction goroutine starts here and stops
// on Close.
func New(cfg config.Config, now func() time.Time) *Cluster {
	if now == nil {
		now = time.Now
	}
	c := &Cluster{
		cfg:            cfg.Cluster,
		poolCfg:        cfg.Pool,
		duckCfg:        cfg.DuckDB,
		readinessQuery: cfg.Status.ReadinessQuery,
		now:            now,
		budget:         newBudget(cfg.Pool.MaxSessions),
		breaker:        newBreaker(cfg.Pool.Breaker.Failures, cfg.Pool.Breaker.OpenFor, now),
		users:          map[string]*userPool{},
		evictStop:      make(chan struct{}),
		evictDone:      make(chan struct{}),
	}
	go c.evict()
	return c
}

// URI is the cluster URI this process fronts.
func (c *Cluster) URI() string { return c.cfg.URI }

// Secured reports whether the cluster has a public key configured, which
// is what makes a login's credentials matter. An insecure cluster accepts
// any user.
func (c *Cluster) Secured() bool { return c.cfg.PublicKeyFile != "" }

// ownCredentials are the REST API's own user. Config carries it as a user
// security file only. Inline credentials exist only for callers, whose
// secrets arrive through the API.
func (c *Cluster) ownCredentials() credentials {
	return credentials{userSecurityFile: c.cfg.UserSecurityFile}
}

// compressionOf and encryptionOf map the config vocabulary onto the
// binding's enums. Validation has already rejected anything else.
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
		WithEncryption(encryptionOf(c.cfg.Encryption))
	if c.cfg.Timeout > 0 {
		o = o.WithTimeout(c.cfg.Timeout)
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

// ErrCredentialsRefused is the error of a dial the secure cluster
// refused: it did not accept the user's name and secret, or the anonymous
// user. The binding's error is wrapped, and its message stays in the
// chain. The binding answers a refused handshake with ErrInvalidReply,
// which it also uses for a confused protocol, so the code alone cannot
// tell the two apart. The classification covers a dial only, so an
// ErrInvalidReply on a push (sc-19944) keeps its own answer.
var ErrCredentialsRefused = errors.New("qdb: the cluster refused the credentials")

// connect dials one session for the user. A budgeted dial takes a budget
// unit first and holds it until the session's close returns. A dial that
// fails gives its unit back at once. On a secure cluster, a dial that fails
// other than by an unreachable cluster is ErrCredentialsRefused.
func (c *Cluster) connect(ctx context.Context, u credentials, budgeted bool) (qdbapi.Session, error) {
	// A dial is three steps, and a failure at any of them leaves the
	// budget as it found it:
	//
	//  1. a budgeted dial takes its unit first;
	//  2. dial;
	//  3. on a failed dial, give the unit back and classify a refusal.

	// 1. take the unit
	if budgeted {
		if err := c.budget.acquire(ctx); err != nil {
			return qdbapi.Session{}, err
		}
	}
	// 2. dial
	hdl, err := qdbapi.NewSessionFactory(c.handleOptions(u)).NewSession()
	// 3. give the unit back and classify a refusal. An insecure cluster
	//    checks no credential, so only a secure cluster's failed dial is
	//    a refusal. The login and the data routes see one classification,
	//    because both dial through here.
	if err != nil {
		if budgeted {
			c.budget.release()
		}
		if c.Secured() && !qdbapi.IsClusterUnavailable(err) {
			err = fmt.Errorf("%w: %w", ErrCredentialsRefused, err)
		}
		return qdbapi.Session{}, err
	}
	return hdl, nil
}

// dial is a user pool's dialer: a budgeted connect.
func (c *Cluster) dial(u credentials) func(context.Context) (qdbapi.Session, error) {
	return func(ctx context.Context) (qdbapi.Session, error) {
		return c.connect(ctx, u, true)
	}
}

// closeBudgeted is the closer of the user pools. It closes the handle and
// only then releases its budget unit, so a session counts against
// max_sessions until qdb_close has returned and the cluster has let go of
// it. The pool runs it on its own goroutine.
func (c *Cluster) closeBudgeted(s qdbapi.Session) error {
	err := s.Close()
	c.budget.release()
	return err
}

// newUserPool builds one user's pool: the per-user cap, the ages from
// config, the cluster clock, the budgeted dialer and closer, and the
// binding's own reaper for idle sessions. The binding rejects options that
// config validation already bounds, so a rejection is a programming error.
func (c *Cluster) newUserPool(u credentials) *qdbapi.SessionPool {
	p, err := qdbapi.NewSessionPool(nil, qdbapi.NewSessionPoolOptions().
		WithMaxSessions(c.poolCfg.PerUserMax).
		WithIdleTimeout(c.poolCfg.IdleTimeout).
		WithMaxLifetime(c.poolCfg.MaxLifetime).
		WithClock(c.now).
		WithDialer(c.dial(u)).
		WithCloser(c.closeBudgeted))
	if err != nil {
		panic("qdb: session pool options rejected: " + err.Error())
	}
	return p
}

// userPoolFor finds or creates the user's pool state. It does not replace
// one that exists, so an in-flight request keeps the pool it leased from.
// The caller holds c.mu.
func (c *Cluster) userPoolFor(u User) *userPool {
	up, ok := c.users[u.Username]
	if !ok {
		up = &userPool{pool: c.newUserPool(u.credentials())}
		c.users[u.Username] = up
	}
	up.emptySince = time.Time{}
	return up
}

// poolFor finds or creates the user's session pool.
func (c *Cluster) poolFor(u User) *qdbapi.SessionPool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userPoolFor(u).pool
}

// Session is one authenticated client session as this package sees it: the
// narrow wrapper around the binding's session and the only way code
// touches one. Every method runs one C API operation, so the surface the
// server depends on is enumerable here. A caller may hold one thing
// afterwards: a query's record batch, which owns its C-allocated buffers
// and frees them on release. A Session is built per checkout. Call wraps
// the session the user's pool leased, and Probe wraps the session it
// dialed itself. One goroutine uses a Session at a time.
type Session struct {
	session qdbapi.Session
}

func newSession(session qdbapi.Session) *Session {
	return &Session{session: session}
}

// closeAsync closes the handle on its own goroutine, because qdb_close
// joins the handle's worker threads and can block for a long time. Only
// the probe's own session ends here. A pooled session ends through its
// lease, and the pool closes it the same way.
func (s *Session) closeAsync() {
	go func() { _ = s.session.Close() }()
}

// fetch runs the query through the C API's Arrow path and returns the
// record batch the binding moved out of it, which holds no reference to
// the session. The caller owns the batch and releases it once. An error
// means no result. A batch handed back with an error (a partial failure)
// is released here, and nil is returned. A statement that produces no
// result set (DDL) yields a nil batch.
func (s *Session) fetch(q string) (arrow.RecordBatch, error) {
	rec, err := s.session.Query(q).FetchArrow()
	if err != nil {
		if rec != nil {
			rec.Release()
		}
		return nil, err
	}
	return rec, nil
}

// describe returns the schema of the query's result without running the
// query. A statement without a result set has the empty schema, which
// matches the empty batch Query hands the sink for it.
func (s *Session) describe(q string) (*arrow.Schema, error) {
	sc, err := s.session.Query(q).ValidateArrow()
	if err != nil {
		return nil, err
	}
	// ValidateArrow returns nil for a statement with no result set.
	if sc == nil {
		return arrow.NewSchema(nil, nil), nil
	}
	return sc, nil
}

// CreateTable creates the table called name, with cols after the implied
// $timestamp column and a shard size of shard.
func (s *Session) CreateTable(name string, shard time.Duration, cols ...qdbapi.TsColumnInfo) error {
	return s.session.Table(name).Create(shard, cols...)
}

// RemoveTable removes the table name.
func (s *Session) RemoveTable(name string) error {
	return s.session.Table(name).Remove()
}

// PushArrow writes every table the writer holds in one batch. The writer
// pins the Go buffers it hands to the C API for the duration of the call.
func (s *Session) PushArrow(w *qdbapi.ArrowWriter) error {
	return w.Push(s.session)
}

// callConfig carries per-call options.
type callConfig struct {
	retry bool
}

// CallOption tunes one Call.
type CallOption func(*callConfig)

// WithReadRetry permits one transparent retry after a retryable failure,
// on a fresh session when the old one went bad. Retry is decided per
// call, because idempotence is a fact only the call site knows.
// Idempotent reads opt in. Ingestion and a response that has started
// streaming do not opt in.
func WithReadRetry() CallOption {
	return func(cc *callConfig) { cc.retry = true }
}

// callerLeft reports whether err is the caller's own context ending. Such
// an error says nothing about the cluster and does not feed the breaker.
func callerLeft(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// feedBreaker tells the breaker what a cluster interaction proved.
func (c *Cluster) feedBreaker(err error) {
	// Only an unreachable cluster is a failure.
	if qdbapi.IsClusterUnavailable(err) {
		c.breaker.recordFailure()
		return
	}
	// A nil error or an answer means the cluster is up, whatever the
	// answer said. A rejected request, a refused credential and a failure
	// in the caller's own Go code are all answers.
	c.breaker.recordSuccess()
}

// Call runs f on a session authenticated as the user. The breaker gates
// the call. The user's pool leases a session, dialing one on demand, and
// runs f. The binding's IsBadSession and Lease.Done decide from f's error
// whether the pool keeps the session. This layer adds two decisions, both
// per cluster. Only errors that are evidence about the cluster
// (IsClusterUnavailable) feed the breaker. An idempotent read can opt in
// to one retry after a retryable failure, and a refused credential is
// never retried. A retry after a bad session runs on a fresh session,
// because the pool discarded the old one. A retry after any other
// retryable failure may run on the same session.
func (c *Cluster) Call(ctx context.Context, u User, f func(*Session) error, opts ...CallOption) error {
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
		err = c.poolFor(u).Do(ctx, func(s qdbapi.Session) error { return f(newSession(s)) })
		if callerLeft(err) {
			return err
		}
		c.feedBreaker(err)
		// A refusal is not retried although the binding calls
		// ErrInvalidReply retryable, because a second dial with the same
		// credentials is refused the same way.
		if !cc.retry || !qdbapi.IsRetryable(err) || errors.Is(err, ErrCredentialsRefused) {
			return err
		}
		cc.retry = false
	}
	return err
}

// slices cuts the batch into a sequence of slices of at most n rows, in
// order. Each slice retains the batch and is released after yield
// returns, so the receiver releases none. The caller owns the batch and
// releases it after the sequence. A batch of no rows is yielded itself,
// once, so the sequence is never empty.
func slices(rec arrow.RecordBatch, n int64) Batches {
	return func(yield func(arrow.RecordBatch, error) bool) {
		// An empty batch is yielded itself, because every receiver expects a
		// batch and the Arrow encoder reads the first batch's schema.
		if rec.NumRows() == 0 {
			yield(rec, nil)
			return
		}
		for start := int64(0); start < rec.NumRows(); start += n {
			s := rec.NewSlice(start, min(start+n, rec.NumRows()))
			// The slice is released after yield returns whether the receiver
			// broke out or not, as lent releases the reader's batches.
			more := yield(s, nil)
			s.Release()
			if !more {
				return
			}
		}
	}
}

// Query runs a native query as the user and hands the result to the sink
// as a sequence of slices of at most model.BatchRows rows. The session is
// back in its pool before the sink runs, so a slow receiver holds no
// session. Every error other than the sink's own is returned before the
// sink is called, and the sink's error is returned as is. A statement
// with no result set yields one empty batch with no columns. Under
// WithReadRetry the query is retried once, as Call retries a read. The
// retry is safe because the query completes before the sink runs.
func (c *Cluster) Query(ctx context.Context, u User, q string, sink func(Batches) error, opts ...CallOption) error {
	// The binding materializes the whole result in one fetch, so the
	// session is needed only for that fetch, and the sink runs after Call
	// has returned the session:
	//
	//  1. fetch the result on a leased session, retried as opts say;
	//  2. stand in an empty batch for a statement with no result set;
	//  3. run the sink over the slices and release the batch afterwards.

	// 1. fetch on a leased session
	var rec arrow.RecordBatch
	err := c.Call(ctx, u, func(s *Session) error {
		var err error
		rec, err = s.fetch(q)
		return err
	}, opts...)
	if err != nil {
		return err
	}
	// 2. a nil batch becomes an empty batch with no columns, because every
	//    receiver expects a batch: Arrow's EncodeStream reads the first
	//    batch's schema, and Batches is never empty.
	if rec == nil {
		rec = model.EmptyBatch(arrow.NewSchema(nil, nil))
	}
	// 3. the batch is released after the sink returns rather than when
	//    yield returns, so a sink that never ranges over the sequence
	//    leaks nothing. Each slice retains the batch and shares its
	//    buffers, so a sink that keeps a slice keeps the buffers. The batch
	//    is sliced here rather than in the encoder, so HTTP and Flight read
	//    batches of one size and a Flight message stays under gRPC's
	//    receive limit.
	defer rec.Release()
	return sink(slices(rec, model.BatchRows))
}

// Describe returns the Arrow schema of a native query's result, as the
// user, without running the query. Field names, types and order match
// what Query returns for the same query. The fields carry no metadata,
// while Query's string and blob fields carry a max_width key. A bare NULL
// literal describes as the null type, where Query returns float64. A
// non-select statement's empty column describes as its declared type,
// where Query returns the null type. The errors are Query's. The
// divergences come from qdbd's validate disagreeing with its own
// execution, and they are accepted (ADR-0018, Decision 9).
func (c *Cluster) Describe(ctx context.Context, u User, q string, opts ...CallOption) (*arrow.Schema, error) {
	// The read retry is as safe here as in Query, because the validate
	// holds a session for one round trip and sends nothing to the caller.
	var sc *arrow.Schema
	err := c.Call(ctx, u, func(s *Session) error {
		var err error
		sc, err = s.describe(q)
		return err
	}, opts...)
	return sc, err
}

// Authenticate checks the user's credentials with one direct dial outside
// the user pools. A pool keeps the credentials it was created with, so a
// lease could only confirm those. The breaker gates the dial and hears its
// outcome. The budget does not count the dial, because the session lives
// for one handshake and closes on its own goroutine. A refused dial is an
// answer and the caller's error, ErrCredentialsRefused on a secure
// cluster. An unreachable cluster is IsClusterUnavailable.
func (c *Cluster) Authenticate(ctx context.Context, u User) error {
	remaining, ok := c.breaker.allow()
	if !ok {
		return &BreakerOpenError{RetryAfter: remaining}
	}
	hdl, err := c.connect(ctx, u.credentials(), false)
	if callerLeft(err) {
		return err
	}
	c.feedBreaker(err)
	if err != nil {
		return err
	}
	newSession(hdl).closeAsync()
	return nil
}

// Probe answers readiness. It dials a fresh session as the REST API's own
// user (outside the pool, the budget and the breaker), runs
// status.readiness_query, and closes the session on its own goroutine. The
// dial proves that the cluster is reachable and that the REST API's own
// user authenticates. The query proves that the session serves queries.
func (c *Cluster) Probe(ctx context.Context) error {
	hdl, err := c.connect(ctx, c.ownCredentials(), false)
	if err != nil {
		return err
	}
	s := newSession(hdl)
	rec, qerr := s.fetch(c.readinessQuery)
	s.closeAsync()
	if rec != nil {
		rec.Release()
	}
	return qerr
}

// InstanceLimitError is the error of a DuckDB call that would open a DuckDB
// instance past duckdb.max_instances. RetryAfter is the eviction tick,
// the soonest an idle instance can close. A configurable RetryAfter was
// rejected as one more knob. The HTTP layer maps the error to 503 with a
// Retry-After of RetryAfter.
type InstanceLimitError struct {
	RetryAfter time.Duration
}

func (e *InstanceLimitError) Error() string {
	return fmt.Sprintf("qdb: duckdb instance limit reached, retry after %s", e.RetryAfter)
}

// credentialsOf returns what the user's instance attaches with. That is
// the first node of the cluster URI and, on a secure cluster, the user's
// credentials and the cluster public key read from its file. On an
// insecure cluster every instance attaches anonymously.
func (c *Cluster) credentialsOf(u User) (olap.Credentials, error) {
	// qdb-duck's ATTACH takes one host and one port, so a multi-node URI
	// is handed over as its first node.
	first, _, _ := strings.Cut(strings.TrimPrefix(c.cfg.URI, "qdb://"), ",")
	host, port, err := net.SplitHostPort(first)
	if err != nil {
		return olap.Credentials{}, fmt.Errorf("cluster.uri %q: %w", c.cfg.URI, err)
	}
	creds := olap.Credentials{Host: host, Port: port}
	// An insecure cluster accepts any user name at login, and qdb-duck
	// refuses a user name without a cluster public key, so the user
	// travels only to a secure cluster.
	if !c.Secured() {
		return creds, nil
	}
	// The key file is read at each open and not cached, because no
	// instance outlives pool.idle_timeout.
	key, err := os.ReadFile(c.cfg.PublicKeyFile)
	if err != nil {
		return olap.Credentials{}, err
	}
	creds.Username, creds.SecretKey = u.Username, u.SecretKey
	creds.ClusterPublicKey = strings.TrimSpace(string(key))
	return creds, nil
}

// instanceOf returns the user's DuckDB instance, opening it on the user's
// first DuckDB call. The open counts against duckdb.max_instances, and past
// the cap the error is an *InstanceLimitError. On a secure cluster, a
// failed attach whose credentials the cluster refuses is
// ErrCredentialsRefused. A failed open is not kept, so the next call tries
// again.
func (c *Cluster) instanceOf(ctx context.Context, up *userPool, u User) (*olap.Instance, error) {
	// The instance is opened once per user and counted against the cap
	// before the open:
	//
	//  1. under duckMu, return the instance when it exists;
	//  2. take a unit of the cap under mu, or fail with InstanceLimitError;
	//  3. open outside mu;
	//  4. on a secure cluster, classify a failed attach by one direct dial;
	//  5. on failure, give the unit back.

	// 1. return the instance when it exists. duckMu is held through the
	//    open, so a second first call waits for this one's instance.
	up.duckMu.Lock()
	defer up.duckMu.Unlock()
	if up.duck != nil {
		return up.duck, nil
	}
	// 2. take a unit of the cap
	c.mu.Lock()
	if c.instances >= c.duckCfg.MaxInstances {
		c.mu.Unlock()
		return nil, &InstanceLimitError{RetryAfter: evictInterval}
	}
	c.instances++
	c.mu.Unlock()
	// 3. open outside mu, because the attach dials the cluster and mu
	//    guards every user
	creds, err := c.credentialsOf(u)
	if err == nil {
		up.duck, err = olap.Open(ctx, c.duckCfg, creds)
	}
	// 4. classify a failed attach on a secure cluster. qdb-duck's message
	//    does not say whether the cluster refused or was unreachable, so
	//    one direct dial as the user asks the cluster. A refused dial
	//    replaces the attach error, and any other outcome keeps
	//    olap.ErrAttach and its 503. The dial runs on this failure path
	//    only. It goes through connect and not Authenticate, because a
	//    DuckDB call does not feed the breaker. Refusing an empty username
	//    in credentialsOf was rejected, because it misses a user removed
	//    after login.
	if errors.Is(err, olap.ErrAttach) && c.Secured() {
		hdl, derr := c.connect(ctx, u.credentials(), false)
		switch {
		case derr == nil:
			newSession(hdl).closeAsync()
		case errors.Is(derr, ErrCredentialsRefused):
			err = derr
		}
	}
	// 5. give the unit back on failure
	if err != nil {
		c.releaseInstance()
		return nil, err
	}
	return up.duck, nil
}

// releaseInstance gives one unit of duckdb.max_instances back.
func (c *Cluster) releaseInstance() {
	c.mu.Lock()
	c.instances--
	c.mu.Unlock()
}

// closeInstance closes the pool's instance, if it has one, and only then
// gives its unit back, so an instance counts against the cap until its
// close returns.
func (up *userPool) closeInstance(c *Cluster) {
	up.duckMu.Lock()
	defer up.duckMu.Unlock()
	if up.duck == nil {
		return
	}
	_ = up.duck.Close()
	up.duck = nil
	c.releaseInstance()
}

// withInstance runs f on the user's DuckDB instance, opening it on the
// user's first call. The call counts as use of the user's pool until f
// returns. An error before f runs comes from instanceOf, and f's error is
// returned unchanged.
func (c *Cluster) withInstance(ctx context.Context, u User, f func(*olap.Instance) error) error {
	// The call counts as use of the user's pool from before the instance
	// opens until after f returns, on every path, so eviction never closes
	// an instance that a call is opening or using:
	//
	//  1. find or create the user's pool and count the call, under one
	//     hold of mu, so no eviction pass sees the pool in between;
	//  2. get the user's instance, opening it on the first call;
	//  3. run f on the instance;
	//  4. uncount the call, whatever happened.

	// 1. find the pool and count the call
	c.mu.Lock()
	up := c.userPoolFor(u)
	up.duckIn++
	c.mu.Unlock()
	// 4. uncount the call, whatever happened
	defer func() {
		c.mu.Lock()
		up.duckIn--
		c.mu.Unlock()
	}()
	// 2. get the instance
	in, err := c.instanceOf(ctx, up, u)
	if err != nil {
		return err
	}
	// 3. run f
	return f(in)
}

// Duck runs a DuckDB query on the user's own DuckDB instance and hands its
// batches to the sink. The query runs on a connection of the instance,
// which closes when the sink returns. An error before the sink runs comes
// from the instance cap (*InstanceLimitError), a refused credential on a
// secure cluster (ErrCredentialsRefused), the attach (olap.ErrAttach,
// which IsClusterUnavailable reports) or DuckDB itself. The sink's error
// is returned unchanged. A DuckDB call is never retried and does not feed
// the breaker. The budget does not count it, because qdb-duck pools its
// own qdbd sessions.
func (c *Cluster) Duck(ctx context.Context, u User, q string, sink func(Batches) error) error {
	return c.withInstance(ctx, u, func(in *olap.Instance) error {
		return in.Query(ctx, q, sink)
	})
}

// DuckDescribe returns the Arrow schema of a DuckDB query's result on the
// user's instance, without running the query. Field names, types and order
// match what Duck streams for the same query. A text that is not one
// statement is olap.ErrNotOneStatement, a query with parameters is
// olap.ErrParameters, and a column type the describe does not map is
// olap.ErrUndescribable. The other errors are Duck's before its sink runs.
func (c *Cluster) DuckDescribe(ctx context.Context, u User, q string) (*arrow.Schema, error) {
	// The describe counts as use of the pool, as Duck does, because it
	// holds a connection of the instance.
	var sc *arrow.Schema
	err := c.withInstance(ctx, u, func(in *olap.Instance) error {
		var err error
		sc, err = in.Describe(ctx, q)
		return err
	})
	return sc, err
}

// evict removes user pools that have held no session for idle_timeout, on
// a fixed tick until Close.
func (c *Cluster) evict() {
	defer close(c.evictDone)
	ticker := time.NewTicker(evictInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.evictStop:
			return
		case <-ticker.C:
			c.evictOnce()
		}
	}
}

// takeIdle removes and returns the user pools that have held nothing for
// idle_timeout. The first pass that finds a pool empty stamps emptySince.
// A later pass past idle_timeout takes the pool. A pool that holds
// anything again loses the stamp. A pool's own reaper closes its idle
// sessions, so a pool reads as empty within one reap tick of its last
// session expiring. A DuckDB call on the pool's instance is use of the pool.
// An idle instance does not keep a pool, so the instance closes with its
// pool after idle_timeout. A separate idle timeout for instances was
// rejected as one more knob.
func (c *Cluster) takeIdle() []*userPool {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	var taken []*userPool
	for name, up := range c.users {
		s := up.pool.Stats()
		if s.InUse+s.Idle+s.Closing+s.Dialing+up.duckIn > 0 {
			up.emptySince = time.Time{}
			continue
		}
		if up.emptySince.IsZero() {
			up.emptySince = now
			continue
		}
		if now.Sub(up.emptySince) >= c.poolCfg.IdleTimeout {
			delete(c.users, name)
			taken = append(taken, up)
		}
	}
	return taken
}

// evictOnce closes the idle user pools, which stops their reapers, and
// then their instances. A taken pool holds no session, so its close
// returns at once unless a caller took it from poolFor in between and its
// dial is in flight.
func (c *Cluster) evictOnce() {
	for _, up := range c.takeIdle() {
		_ = up.pool.Close(context.Background())
		up.closeInstance(c)
	}
}

// Reap runs one eviction pass. The tests drive it against a fake clock
// instead of waiting for the ticker.
func (c *Cluster) Reap() { c.evictOnce() }

// ClusterStats is a snapshot of the whole cluster's session and DuckDB
// instance usage.
type ClusterStats struct {
	BudgetInUse int
	BudgetMax   int
	Users       int
	Instances   int
}

// Stats returns a snapshot.
func (c *Cluster) Stats() ClusterStats {
	c.mu.Lock()
	users, instances := len(c.users), c.instances
	c.mu.Unlock()
	return ClusterStats{
		BudgetInUse: c.budget.inUse(),
		BudgetMax:   c.budget.max(),
		Users:       users,
		Instances:   instances,
	}
}

// Close first stops eviction and waits for its goroutine to end, so no
// pass runs during the drain. Then it takes every user pool out of the
// map, drains each within ctx and closes its instance, and it returns
// ctx.Err() for whatever has not closed in time. The process exits
// regardless.
func (c *Cluster) Close(ctx context.Context) error {
	close(c.evictStop)
	<-c.evictDone
	c.mu.Lock()
	pools := maps.Values(c.users)
	c.users = map[string]*userPool{}
	c.mu.Unlock()
	var err error
	for up := range pools {
		if e := up.pool.Close(ctx); e != nil && err == nil {
			err = e
		}
		up.closeInstance(c)
	}
	return err
}
