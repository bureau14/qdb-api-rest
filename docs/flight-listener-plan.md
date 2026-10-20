# Plan: flight listener -- the Flight SQL listener with Handshake and bearer auth

Status: draft

This plan answers the first Next item of `docs/log.md`, Current state,
in its first slice: the Flight SQL listener, Handshake, the bearer and
the anonymous rule. The statement query is the next slice. It is
written for an executor who has none of the seed conversation of
2026-10-20. Everything the build needs is here.

## 1. Outcome

When the unit lands:

- Every start of the binary serves a TLS gRPC listener on
  `listen.flight`, default `:40493`, with the certificates of
  `tls.certificate` and `tls.private_key` and the HTTPS listener's
  ephemeral fallback. An empty `listen.flight` refuses the start by key.
- `internal/flightsql` embeds arrow-go's Flight SQL base server. Every
  Flight SQL command answers `UNIMPLEMENTED`, and Handshake is the one
  RPC that does work.
- A client logs in with `authorization: Basic base64(user:secret)` on
  the Handshake and reads the access token back from the reply's
  `authorization: Bearer <token>` header. Every later call presents
  that header. A call with no header is the anonymous user on an
  insecure cluster and `UNAUTHENTICATED` on a secure one. A refused
  credential, an expired, foreign or refresh token, and a malformed
  header are `UNAUTHENTICATED`.
- Every RPC writes one access line, and the lines below an
  authenticated call carry the user and the session.
- The listener drains on SIGTERM next to the HTTP listeners under the
  one `shutdownGrace`, and a Flight listener failure stops the binary
  as an HTTP one does.
- `vendor/` carries arrow-go's flight packages, grpc-go and protobuf at
  the versions arrow-go v18.8.0 requires.
- One Go test in `internal/flightsql` proves login, bearer, anonymous
  and refusal through arrow-go's client on both clusters.

Left for later units, each named:

- the statement query: `GetFlightInfoStatement` answering a ticket that
  carries the query text, `DoGetStatement` through `query.Run`, and the
  `INVALID_ARGUMENT`, `UNAVAILABLE` and `INTERNAL` rows of ADR-0018,
  Decision 13, for a running query;
- `GetSqlInfo` and the catalog commands on a table list core;
- the format-equivalence property with a Flight client and the ADBC and
  JDBC smoke tests (`docs/e2e.md`);
- query capture and monitoring on both planes, in M6 (owner,
  2026-10-20; section 4).

## 2. Verified facts

1. No Flight code exists: there is no `internal/flightsql`, no
   `listen.flight` in `internal/config/config.go:38-43`, and `vendor/`
   holds no `arrow/flight`, `google.golang.org/grpc` or
   `google.golang.org/protobuf` package [session of 2026-10-20,
   `ls vendor/github.com/apache/arrow-go/v18/arrow/`,
   `vendor/modules.txt`].
2. arrow-go v18.8.0, the vendored release, ships `arrow/flight` and
   `arrow/flight/flightsql` and requires `google.golang.org/grpc
v1.83.2` and `google.golang.org/protobuf v1.36.12`
   [`$GOMODCACHE/cache/download/github.com/apache/arrow-go/v18/@v/v18.8.0.mod:52-53`].
   `go mod vendor` vendors a package only once the module imports it,
   so the vendor change lands in the commit that adds the first import.
3. `flightsql.BaseServer` answers every Flight SQL method with
   `UNIMPLEMENTED` and must be embedded; `flightsql.NewFlightServer`
   wraps a `flightsql.Server` into a `flight.FlightServer`, and
   `flight.RegisterFlightServiceServer` registers it on a `*grpc.Server`
   [`$GOMODCACHE/github.com/apache/arrow-go/v18@v18.8.0/arrow/flight/flightsql/server.go:287-295,316-577,724-726`,
   `arrow/flight/server.go:170`].
4. `flight.BaseFlightServer.Handshake` returns nil at once when no
   `ServerAuthHandler` is set, so the Handshake's work can live in an
   interceptor and the RPC body stays the base one
   [`arrow/flight/server.go:243-249`].
5. arrow-go's shipped basic-auth middleware refuses every call whose
   `authorization` metadata is absent (`must authenticate first`),
   handles a `Basic` credential on `/Handshake` by decoding
   `user:secret` (raw or padded base64), calls `Validate` and sets the
   token as a trailer, and verifies a `Bearer` on every other call
   [`arrow/flight/server_auth.go:146-232`]. It cannot admit the
   anonymous user, so the unit writes its own interceptor in that shape
   (section 4).
6. arrow-go's client `AuthenticateBasicToken` sends `Authorization:
Basic <raw base64 user:secret>` on the Handshake, closes send, reads
   the header, tolerates no message and EOF, and takes the token from
   the joined header and trailer
   [`arrow/flight/client.go:340-372`]. The JDBC driver reads the token
   from the response headers only (`ClientIncomingAuthHeaderMiddleware.
onHeadersReceived`, trailers unread) [apache/arrow-java `main`,
   fetched 2026-10-20]. The ADBC Flight SQL driver does no
   authentication unless `username` and `password` or
   `adbc.flight.sql.authorization_header` are set, and the JDBC driver
   sends no auth header unless `user`, `token` or an OAuth config is
   set [ADBC driver documentation and
   `ArrowFlightSqlClientHandler.java`, fetched 2026-10-20].
7. grpc-go's `ServerStream.SendHeader` sends the headers at once as a
   HEADERS frame, where a header set by `SetHeader` alone and no
   message goes out with the status as a trailers-only reply
   [`google.golang.org/grpc/stream.go`, `ServerStream` doc, the
   vendored copy after commit 3]. The Handshake therefore calls
   `SendHeader`, so a client that reads headers only (fact 6) sees the
   token.
8. `grpc.Server.GracefulStop` blocks until every RPC has finished with
   no deadline, and `Stop` cuts them [`google.golang.org/grpc/server.go`,
   the vendored copy after commit 3]. arrow-go's `flight.Server` helper
   exposes `Shutdown` as a bare `GracefulStop` and no forced stop
   [`arrow/flight/server.go:395-397`], so the package builds its own
   `*grpc.Server`.
9. The entry point builds `[]*http.Server`, serves each on a goroutine
   until the first terminal error or the root context ends, treats
   `http.ErrServerClosed` as clean, and shuts down under one
   `shutdownGrace` of eight seconds, servers first and the cluster
   second [`cmd/qdb_rest/main.go:51-130`]. The grace must stay below
   the harness's SIGTERM-to-SIGKILL window [`cmd/qdb_rest/main.go:50`].
10. `tlsconf.Load(cfg.TLS, now)` answers a `*tls.Config`, an `Info`
    with `Source` and `Fingerprint`, and the entry point logs the
    ephemeral warning once for the HTTPS listener
    [`internal/tlsconf/tlsconf.go:119`, `cmd/qdb_rest/main.go:74-84`].
11. `validate` refuses both HTTP addresses empty, and the bounds a value
    must meet live in `internal/config` by key
    [`internal/config/config.go:621-623`, `internal/AGENTS.md`, Code].
    `examples/qdb_rest.yaml` is pinned to `Default()` by test
    [`internal/config/config_test.go:248-253`].
12. The HTTP bearer edge is `requireBearer`: no header is a bare
    challenge, a malformed header or a failed verify is `invalid_token`,
    `typ != access` is refused, and the claims ride the ctx under
    `auth.WithClaims` with `observe.KeyUser` and `observe.KeySession`
    tagged [`internal/httpapi/bearer.go:46-85`]. `auth.ClaimsFrom`
    panics without claims [`internal/httpapi/AGENTS.md`, Handlers].
    Handlers derive the cluster user from the claims
    [`internal/httpapi/tables.go:75-79`].
13. The HTTP login proves credentials by `Cluster.Authenticate`, treats
    an empty username as the anonymous user, mints through
    `Tokens.Issue`, answers `ErrCredentialsRefused` as the caller's 401
    and everything else through `writeClusterError`, with a mint
    failure 500 [`internal/httpapi/login.go:28-35,73-108`].
14. `Cluster.Authenticate` is one dial outside the pools, gated by the
    breaker, answering `*BreakerOpenError` while open,
    `ErrCredentialsRefused` on a secure cluster's refusal and
    `qdbapi.IsClusterUnavailable` for an unreachable cluster
    [`internal/qdb/cluster.go:157-200,436-458`].
15. `Tokens.Verify` answers `ErrTokenExpired` for a genuine expired
    token and `ErrInvalidToken` for everything else, and `Claims`
    carries `Username`, `SecretKey`, `SessionID` and `Typ`
    [`internal/auth/token.go:23-49,217`].
16. The HTTP access line is one `request` line per request with
    `method`, `path`, `status`, `bytes`, `duration_ms` and the request
    id, minted as a UUIDv7 when the client sent none, under the
    `observe.KeyRequestID` tag, and it carries no user tag because the
    bearer edge runs inside it [`internal/httpapi/middleware.go:69-83`].
17. The logging rule for gRPC is already written: interceptors tag the
    ctx, and the code below only logs [`internal/AGENTS.md`, Logging,
    "Edges enrich"]. The logger, the cluster and the keychain travel in
    the context and `Logger` and `ClusterFrom` panic without them
    [`internal/AGENTS.md`, Logging].
18. The handler tests build the ctx as the entry point does, with a
    discarding logger, the fixture cluster and an ephemeral keychain,
    run on both clusters through `cluster.Each`, and mint tokens
    directly [`internal/httpapi/query_test.go:31-57`,
    `internal/qdbtest/cluster/cluster.go:29-56`].
19. golangci-lint excludes `vendor` [`.golangci.yml:27`], and the CI
    steps run `go build`, lint and `go test -p 1 ./...` on every
    platform [`scripts/cicd/`]. grpc-go and protobuf are pure Go, so
    the platform matrix needs nothing new.

## 3. Design

### `internal/config/config.go`, the `Listen` block (line 40)

```go
// Listen holds the listener addresses. An empty HTTP or HTTPS address disables
// that listener. The Flight address is never empty, because the Flight SQL
// listener is on every install and the port is the configurable part. [ADR-0018, Decision 1]
type Listen struct {
	HTTP   string `yaml:"http" ...`
	HTTPS  string `yaml:"https" ...`
	Flight string `yaml:"flight" help:"Flight SQL (gRPC, TLS) listener address; always on"`
}
```

`Default()` (line 174) gains `Flight: ":40493"` [ADR-0018, Decision 1].

`validate` (line 621), one check after the two-HTTP rule:

```go
	// at the Flight check: an empty address is refused by key, because the listener is
	//   on every install (ADR-0018, Decision 1) and an operator who wants it off has no
	//   knob [Rationale: the empty address]
	if cfg.Listen.Flight == "" { return errors.New("listen.flight must be set: ...") }
```

`examples/qdb_rest.yaml`, `listen:` gains the key with a two-line
comment (always on, the port is configurable). `config_test.go`'s
default pin needs no edit; a validation row for the empty address joins
the existing validation test.

### `internal/flightsql/server.go`

```go
// Package flightsql serves Arrow Flight SQL on its own TLS gRPC port. The
// server embeds arrow-go's base server, so every command it does not
// implement answers UNIMPLEMENTED, and it stores nothing between calls
// (ADR-0018). [facts 3, 8]
package flightsql
```

```go
// Server is the Flight SQL service and the gRPC server that carries it. [fact 3]
type Server struct {
	flightsql.BaseServer
	root context.Context  // the process context: logger, cluster, keychain [fact 17]
	grpc *grpc.Server
	lis  net.Listener
}
```

```go
// New builds the server over the process context ctx, bound to addr with tlsConfig,
// and listens. ctx carries the logger, the cluster and the keychain every call reads. [fact 17]
// The listener is TLS only. [ADR-0018, Decision 1]
func New(ctx context.Context, addr string, tlsConfig *tls.Config) (*Server, error) {
	// style: doc comment + numbered overview + step comments
	// because: the interceptor order is a rule, and the server is built by hand for one reason
	//
	// The gRPC server is built directly rather than through arrow-go's helper, because the
	// helper's shutdown is a bare GracefulStop with no forced stop after the grace [fact 8;
	// Rationale: own grpc.Server]
	//
	// 1. the interceptor chain: the access line outermost, then auth, so a refused call is
	//    logged too, and the access line carries no user tag, as the HTTP line does not
	//    [fact 16; Rationale: interceptor order]
	// 2. the TLS credentials from tlsConfig, and no plaintext option [ADR-0018, Decision 1]
	// 3. register the Flight SQL service wrapped by flightsql.NewFlightServer [fact 3]
	// 4. listen on addr, so a port in use fails the start here [fact 9]
}
```

```go
// Addr answers the bound address, for a test that listens on port zero.
func (s *Server) Addr() net.Addr {
	// style: doc comment alone
	// because: an accessor
}
```

```go
// Serve serves until Shutdown or a listener failure, answering nil after Shutdown. [fact 8]
func (s *Server) Serve() error {
	// style: doc comment alone
	// because: one forwarding call
}
```

```go
// Shutdown stops accepting calls and waits for the calls in flight until ctx ends, then
// cuts the rest. [fact 8; Rationale: the drain]
func (s *Server) Shutdown(ctx context.Context) {
	// style: doc comment + inline note at the race
	// because: two steps whose order is the whole point
	//
	// at the goroutine: GracefulStop has no deadline of its own, so it runs on a goroutine
	//   and the select below bounds it [fact 8]
	// at the ctx branch: Stop cuts the calls still open when the grace ends, so shutdown
	//   never outlives the harness's SIGKILL window [fact 9]
}
```

```go
// callContext gives a call's ctx the process values: the logger, the cluster and the
// keychain, each copied explicitly so a reader sees what a handler may read. [fact 17]
func (s *Server) callContext(ctx context.Context) context.Context {
	// style: doc comment alone
	// because: three assignments
	//
	// at the body: grpc-go has no BaseContext as net/http has, so the interceptor splices
	//   the process values onto the call's cancellation [Rationale: callContext]
}
```

### `internal/flightsql/logging.go`

```go
// accessUnary and accessStream write one access line per RPC: the method, the gRPC
// code, the duration and a minted request id, in the HTTP access line's vocabulary. [fact 16; Rationale: the access line]
func (s *Server) accessUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error)
func (s *Server) accessStream(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error
```

Both:

```go
	// style: doc comment alone
	// because: tag, call, log, as withRequestLogging does
	//
	// at the id: minted as a UUIDv7 on every call, because no stock driver sends one and
	//   the inbound-id rule lives in httpapi [Rationale: the request id]
	// at the line: the code comes from status.Code(err), so a handler's error and a
	//   refused call log the same way [fact 16]
```

```go
// wrappedStream carries a replaced ctx through a stream, because grpc.ServerStream's
// Context is a method. [fact 5, the shape of arrow-go's]
type wrappedStream struct { grpc.ServerStream; ctx context.Context }
func (w *wrappedStream) Context() context.Context { return w.ctx }
```

### `internal/flightsql/auth.go`

```go
// credential reads the authorization metadata of a call: its scheme, its value and
// whether a header was present at all. The scheme is matched case-insensitively. [fact 5; fact 12]
func credential(ctx context.Context) (scheme, value string, present bool) {
	// style: doc comment alone
	// because: one metadata read and one Cut, as bearerToken in httpapi
}
```

```go
// unauthenticated is the UNAUTHENTICATED status with detail, which names the failure
// as the HTTP challenge's detail does. [ADR-0018, Decision 13; fact 12]
func unauthenticated(detail string) error {
	// style: doc comment alone
	// because: one status call
}
```

```go
// authenticate decides who a call runs as, from its authorization metadata, and answers
// the ctx the handler runs under: the claims placed, the user and the session tagged. [ADR-0018, Decisions 7, 8]
// No header is the anonymous user on an insecure cluster and UNAUTHENTICATED on a secure
// one. A present header must be a Bearer with a verifiable access token. [Rationale: own interceptor]
func (s *Server) authenticate(ctx context.Context) (context.Context, error) {
	// style: doc comment + numbered overview + step comments
	// because: a precedence of four outcomes, each a rule of ADR-0018
	//
	// 1. no header: anonymous on an insecure cluster, because the stock drivers send no
	//    header unless configured and a zero-config client must work; UNAUTHENTICATED on a
	//    secure one, because the cluster will refuse the anonymous dial anyway and the
	//    client should log in first [fact 6; ADR-0018, Decision 8]
	//    - rejected: arrow-go's middleware, because it refuses every call without a
	//      header [fact 5; Rationale: own interceptor]
	// 2. a header that is empty, not Bearer, or has no value: UNAUTHENTICATED, because a
	//    client that sent a header meant to authenticate [owner, 2026-10-20]
	// 3. verify: an expired token and an invalid one both UNAUTHENTICATED with the detail
	//    naming which, as the HTTP edge does [fact 12, fact 15]
	// 4. typ != access refused, because a refresh token opens the refresh endpoint and
	//    nothing else [ADR-0018, Decision 7; ADR-0016]
	// 5. the claims ride the ctx, and the logger is tagged with the user and the session,
	//    so the code below only logs [fact 12; fact 17]
	//    the anonymous ctx carries zero Claims, so ClaimsFrom never panics and the next
	//    slice derives the user as httpapi does [fact 12]
}
```

```go
// login proves a Basic credential by one dial and answers the access token. An empty
// username is the anonymous login, as the HTTP login treats it. [fact 13; ADR-0018, Decision 6]
func (s *Server) login(ctx context.Context, username, secret string) (string, error) {
	// style: doc comment + numbered overview + step comments
	// because: one dial with four outcomes mapped to codes
	//
	// 1. the user: an empty username carries no secret, whatever was sent [fact 13]
	// 2. Authenticate: a refusal is UNAUTHENTICATED with the binding's message, an open
	//    breaker or an unreachable cluster is UNAVAILABLE, a caller that went away is
	//    CANCELED, and the breaker's retry-after is not carried, because gRPC has no
	//    Retry-After header and the next slice decides the detail [fact 14; ADR-0018,
	//    Decision 13]
	// 3. Issue: the access token alone, because Flight has no refresh path; a mint
	//    failure is INTERNAL [ADR-0018, Decisions 6, 13]
}
```

```go
// handshake answers a Handshake call: it decodes the Basic credential, logs in, and sends
// the access token as a Bearer in the reply's headers before the base RPC runs. [facts 4, 6, 7]
func (s *Server) handshake(ctx context.Context, stream grpc.ServerStream, scheme, value string, present bool) error {
	// style: doc comment + numbered overview + step comments
	// because: three decodes and one send whose timing matters
	//
	// 1. a Handshake without a credential is UNAUTHENTICATED, because the call exists to
	//    log in [fact 5]
	// 2. decode: the scheme must be Basic, the value raw or padded base64, the text
	//    user:secret, as arrow-go's client and both drivers send it [fact 5, fact 6]
	// 3. login
	// 4. SendHeader with authorization: Bearer <token>, not SetHeader, because a header
	//    set and never flushed goes out trailers-only and the JDBC driver reads headers
	//    only [fact 6, fact 7]
}
```

```go
// authUnary and authStream are the auth edge: a Handshake stream goes to handshake, and
// every other call runs under authenticate's ctx. [ADR-0018, Decisions 6 to 8]
func (s *Server) authUnary(...)
func (s *Server) authStream(...)
```

Both:

```go
	// style: doc comment + inline note at the Handshake branch
	// because: one branch on the method, then a forward
	//
	// at the Handshake branch: matched by the method's suffix, as arrow-go matches it [fact 5]
	// at the forward: the call ctx is callContext first, so authenticate can read the
	//   cluster and the keychain [fact 17]
```

### `cmd/qdb_rest/main.go`

```go
// listener is one of the binary's servers as the serve loop and shutdown see it. [fact 9]
type listener interface {
	Serve() error
	Shutdown(context.Context)
}
```

```go
// httpListener adapts *http.Server: ListenAndServe or ListenAndServeTLS by TLSConfig,
// ErrServerClosed as a clean stop, and Shutdown's error dropped as it is today. [fact 9]
type httpListener struct{ *http.Server }
```

`newServers` becomes `newListeners(ctx, cfg, handler) ([]listener, error)`:

```go
	// style: doc comment + numbered overview + step comments
	// because: three listeners, one TLS load, one warning
	//
	// 1. HTTP, when configured [unchanged]
	// 2. the TLS material, loaded once for HTTPS and Flight, and the ephemeral warning
	//    logged once, because both listeners serve the same certificate [fact 10;
	//    ADR-0018, Decision 1]
	// 3. HTTPS, when configured [unchanged]
	// 4. Flight, always, through flightsql.New, logged as proto "flight" with the
	//    certificate source as the HTTPS line is [ADR-0018, Decision 1]
```

`serveUntilDone` and `shutdown` take `[]listener`. `shutdown`'s doc
comment gains one claim: the Flight server drains in parallel with the
HTTP servers under the same grace, and the cluster closes after all of
them, because in-flight calls on either plane hold sessions [Rationale:
the drain]. The package doc of `main` names three listeners.

### `internal/flightsql/flightsql_test.go`

```go
// server is the Flight server on a port of its own over the context main would give it,
// with an ephemeral certificate, plus the fixture's user and a token for it. [fact 18]
type server struct { ... }

// newServer starts the server for f on 127.0.0.1:0 and stops it on cleanup. [fact 10]
func newServer(t *testing.T, f cluster.Fixture) server

// dial opens arrow-go's Flight SQL client with certificate verification off, because the
// certificate is ephemeral. [docs/e2e.md, The Flight SQL smoke tests]
func (s server) dial(t *testing.T) *flightsql.Client
```

`TestHandshake`, on both clusters through `cluster.Each`:

```go
	// 1. AuthenticateBasicToken with the fixture's user answers a ctx carrying a Bearer
	//    the keychain verifies as an access token for that user [fact 6; ADR-0018, Decision 6]
	// 2. Execute under that ctx answers UNIMPLEMENTED, which proves the bearer passed and
	//    the statement query is not yet there [fact 3]
	// 3. Execute with no header answers UNIMPLEMENTED on insecure and UNAUTHENTICATED on
	//    secure [ADR-0018, Decision 8]
	// 4. AuthenticateBasicToken with a wrong secret answers UNAUTHENTICATED on secure
	//    only, because the insecure cluster checks no credential [fact 14]
```

`TestBearerRows`, a table on the insecure cluster, because none dials:
an expired access token, a refresh token, a tampered token, a non-Bearer
scheme and an empty value each answer `UNAUTHENTICATED` with the
expected detail [fact 12, fact 15]. Tokens are minted directly through
a keychain with a fixed clock, as `bearer_test.go` does.

### `internal/flightsql/AGENTS.md`

```
# internal/flightsql -- Agent Instructions
Scope: the Flight SQL server. Package-wide rules, logging and fixtures: internal/AGENTS.md.
ADR-0018 holds the wire contract.

## Server
- the gRPC server is built by hand, not through arrow-go's helper, because the helper
  cannot force-stop after the grace [Rationale: own grpc.Server]
- interceptor order: access line outermost, auth inside; the access line carries no
  user tag, as the HTTP line does not [Rationale: interceptor order]
- the call ctx gets the process values through callContext, explicitly [Rationale: callContext]
- the request id is minted per call, never read from metadata [Rationale: the request id]

## Auth
- the one rule of authenticate, and that it is this package's own interceptor
  because arrow-go's refuses a missing header [Rationale: own interceptor]
- Handshake: Basic in, Bearer out through SendHeader, and why SendHeader [fact 7]
- a Handshake answers the access token only; no refresh on Flight [ADR-0018, Decision 6]
- the codes of a login: UNAUTHENTICATED, UNAVAILABLE, CANCELED, INTERNAL [ADR-0018, Decision 13]

## Tests
- one test file, arrow-go's client, certificate verification off, both clusters through
  cluster.Each, tokens minted directly [fact 18]
```

### `internal/AGENTS.md`

Code, after the `internal/httpapi` bullet:

```
- The Flight SQL server lives in `internal/flightsql`. Open `internal/flightsql/AGENTS.md`
  before touching an interceptor, the Handshake or the listener.
```

Building, after the `qdb-api-go` sentences:

```
- grpc-go and protobuf are vendored at the versions arrow-go requires, and nothing pins
  them on their own; bumping arrow-go is what moves them, through `go mod tidy` and
  `go mod vendor`. [owner, 2026-10-20; Rationale: the grpc version]
```

### `cmd/qdb_rest/AGENTS.md`

One bullet: the binary serves three listeners, and the Flight one is
always on, so `--listen-flight` has no empty form [ADR-0018, Decision
1; Rationale: the empty address].

### `docs/log.md`, Current state

In flight: the slice's line leaves. Next, item 1 becomes the statement
query alone. Handoff to M6 gains:

```
- Query capture and monitoring on both planes: every executed query and its duration,
  possibly sampled, is M6's (owner, 2026-10-20). Nothing logs query text today.
```

Next, item 4 and the M5 criterion's last clause (the `tls_port` sample
configs in `qdb-documentation`) leave: `qdb-documentation` is outside
this project and is tackled after the official release (owner,
2026-10-20). The same sentence leaves `docs/brief.md`, Milestones, M5,
and ADR-0018's Consequences (section 8, question 1). One log entry
records the owner decision.

## 4. Rationale

| decision                                                                                                                                      | why                                                                                                                                                                                                       | rejected, and why                                                                                                                                                                     | gained                                                              | given up                                                                                         | settled by        |
| --------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ | ----------------- |
| Handshake takes `authorization: Basic` and answers `authorization: Bearer` in the headers                                                     | the form arrow-go's client and both stock drivers send, so ADBC and JDBC log in with their built-in options (fact 6)                                                                                      | credentials in the Handshake payload: no stock driver sends it, so the Python API and customers would need custom auth code                                                           | zero custom client code                                             | the payload bytes are ignored                                                                    | owner, 2026-10-20 |
| own interceptor: no header is anonymous on an insecure cluster, UNAUTHENTICATED on a secure one; a present header must be a verifiable Bearer | arrow-go's middleware refuses every call without a header (fact 5), and the stock drivers send none unless configured (fact 6), so the anonymous rule of ADR-0018 Decision 8 needs the package's own edge | arrow-go's middleware as shipped: an anonymous client would have to Handshake with empty credentials, reversing Decision 8                                                            | zero-config anonymous clients                                       | one interceptor file in the shape of the library's                                               | owner, 2026-10-20 |
| an empty `listen.flight` is refused at startup by key                                                                                         | the listener is on every install and the port is the configurable part (ADR-0018, Decision 1)                                                                                                             | empty disables, as the HTTP keys: one rule across the block, but it turns the always-on decision into a default                                                                       | the ADR holds as written                                            | no knob to turn Flight off                                                                       | owner, 2026-10-20 |
| one access line per RPC, user and session tags, no query text                                                                                 | parity with the HTTP plane's `request` line (fact 16), and query text is logged on neither plane today                                                                                                    | tags only, no line: a second interceptor later anyway; query text at debug on both planes: a decision on what the line carries, and the owner wants it designed with monitoring in M6 | every RPC is visible in the log                                     | nothing                                                                                          | owner, 2026-10-20 |
| query capture and monitoring is M6's                                                                                                          | the owner wants every executed query and its duration captured, possibly sampled, as part of observability                                                                                                | in this unit: it is a design of its own for both planes                                                                                                                               | one design for both planes                                          | nothing logs query text until then                                                               | owner, 2026-10-20 |
| the request id is minted per call, never read from metadata                                                                                   | no stock driver sends one (unverified for every driver, so stated as: none of ADBC, JDBC or arrow-go's client sets one by default), and the inbound-id validation lives in `httpapi`                      | honoring an inbound `x-request-id`: moving `validRequestID` to `observe` is a refactor the unit does not need                                                                         | the smallest edge                                                   | a load balancer's id does not join a Flight line                                                 | proposal          |
| the Flight server drains in parallel with the HTTP servers under the one `shutdownGrace`, then the cluster closes                             | the brief's Resilience says the graceful drain covers Flight streams, and in-flight calls on either plane hold sessions (fact 9)                                                                          | a hard stop: M6's drain would undo it                                                                                                                                                 | one rule for every listener, the harness's SIGKILL window unchanged | nothing                                                                                          | owner, 2026-10-20 |
| own `*grpc.Server` rather than arrow-go's `flight.Server` helper                                                                              | the helper's `Shutdown` is a bare `GracefulStop` with no forced stop, and `GracefulStop` has no deadline (fact 8)                                                                                         | the helper: it would need a second path to `Stop`, which it does not expose                                                                                                           | a drain that ends when the grace ends                               | the helper's `Init` and signal plumbing, which the entry point has anyway                        | proposal          |
| the access line is the outermost interceptor, auth inside, and the line carries no user tag                                                   | a refused call is logged too, and the HTTP line is shaped the same because the bearer edge runs inside it (fact 16)                                                                                       | auth outermost, so the line carries the user: a refused call would then be logged by the auth edge or not at all                                                                      | the same vocabulary on both planes                                  | the user is on the lines below the access line, not on it                                        | proposal          |
| `callContext` copies the logger, the cluster and the keychain onto the call ctx explicitly                                                    | grpc-go has no `BaseContext`, and the project reads process values from the ctx and injects nothing (fact 17)                                                                                             | a merged-context type that answers values from the root and cancellation from the call: more code, and implicit about what a handler may read                                         | a reader sees the three values a handler may read                   | one line per value when a fourth joins                                                           | proposal          |
| a `listener` interface in `main`, `httpListener` adapting `*http.Server`                                                                      | the serve loop and `shutdown` need one shape for three servers (fact 9)                                                                                                                                   | `[]*http.Server` plus a separate Flight branch in both functions: two loops to keep in step                                                                                           | one loop, one drain                                                 | one small type                                                                                   | proposal          |
| the TLS material is loaded once for HTTPS and Flight                                                                                          | both listeners serve the same certificate (ADR-0018, Decision 1), and the ephemeral warning should log once                                                                                               | `tlsconf.Load` twice: two ephemeral certificates with two fingerprints on one start                                                                                                   | one identity per start                                              | none                                                                                             | proposal          |
| grpc-go and protobuf at the versions arrow-go requires, nothing pinning them on their own                                                     | the owner has no preference as long as they are in sync with arrow-go                                                                                                                                     | a direct `require` line pinning each: a second place to bump                                                                                                                          | one bump moves the three                                            | none                                                                                             | owner, 2026-10-20 |
| the vendor change lands in the commit that adds the first import                                                                              | `go mod vendor` vendors only imported packages (fact 2), so a vendor-only commit cannot exist                                                                                                             | a vendor commit with a blank import: a lint finding for nothing                                                                                                                       | one commit that builds                                              | a large diff next to a small file; the owner reads `go.mod` and the new file and skips `vendor/` | proposal          |
| the test is one file, arrow-go's client, both clusters, tokens minted directly                                                                | ADR-0018 Decision 14 names arrow-go's client, and the handler tests set the shape (fact 18)                                                                                                               | a property test over generated credentials: nothing generative in a login's four outcomes                                                                                             | the shape every later Flight test extends                           | none                                                                                             | proposal          |
| `qdb-documentation` work is outside this project, after the official release                                                                  | the owner's word                                                                                                                                                                                          | keeping the `tls_port` item in the M5 criteria                                                                                                                                        | an M5 exit the tree can meet                                        | nothing                                                                                          | owner, 2026-10-20 |

## 5. Knowledge

Commit 2, `listen.flight`:

- the `Listen` type comment: the Flight address is never empty and why
  (ADR-0018, Decision 1; Rationale: the empty address);
- the inline note at the validation check (Rationale: the empty
  address);
- `examples/qdb_rest.yaml`: the two-line comment on the key;
- `cmd/qdb_rest/AGENTS.md`: the bullet on three listeners and the flag
  with no empty form.

Commit 3, the package and the vendor change:

- the package doc: the base server, UNIMPLEMENTED, nothing stored
  (facts 3, 8; ADR-0018, Decision 9);
- `New`'s overview: own `*grpc.Server` and why, the interceptor order
  and why, TLS only (Rationale: own grpc.Server, interceptor order;
  ADR-0018, Decision 1);
- `Shutdown`'s inline notes: the deadline-less `GracefulStop` and the
  forced `Stop` (fact 8, fact 9; Rationale: the drain);
- `callContext`'s inline note (Rationale: callContext);
- `internal/flightsql/AGENTS.md`, Server, every bullet;
- `internal/AGENTS.md`, Code, the pointer bullet, and Building, the
  grpc version rule (Rationale: the grpc version).

Commit 4, the listener in `main`:

- the `listener` and `httpListener` comments (Rationale: the listener
  interface);
- `newListeners`'s overview: one TLS load and one warning (Rationale:
  TLS loaded once);
- `shutdown`'s doc comment claim on the parallel drain (Rationale: the
  drain);
- the package doc of `main`: three listeners.

Commit 5, the auth interceptor:

- `authenticate`'s overview and steps: the four outcomes, arrow-go's
  middleware rejected at step 1, zero claims for anonymous (Rationale:
  own interceptor; ADR-0018, Decisions 7, 8);
- `credential` and `unauthenticated` doc comments (fact 12);
- `internal/flightsql/AGENTS.md`, Auth, the rule and the rejected
  middleware.

Commit 6, the Handshake:

- `login`'s steps: the anonymous username, the code of each outcome,
  the retry-after not carried (fact 13, fact 14; ADR-0018, Decisions 6,
  13);
- `handshake`'s steps: the Basic decode and `SendHeader` over
  `SetHeader` (facts 5, 6, 7);
- `internal/flightsql/AGENTS.md`, Auth, the Handshake bullets.

Commit 7, the access line:

- `accessUnary` and `accessStream` notes: the minted id and the code
  from `status.Code` (fact 16; Rationale: the request id, interceptor
  order);
- `internal/flightsql/AGENTS.md`, Server, the request id bullet.

Commit 8, the test:

- the fixture comments: port zero, verification off (fact 10,
  `docs/e2e.md`);
- the step comments of `TestHandshake` (ADR-0018, Decisions 6, 8;
  fact 14);
- `internal/flightsql/AGENTS.md`, Tests.

Commit 11, `docs/log.md` and the documents:

- Handoff to M6: query capture and monitoring (Rationale: M6);
- Next item 4, the M5 criterion's clause, the brief's M5 sentence and
  ADR-0018's consequence on `qdb-documentation` leave, with one log
  entry (Rationale: qdb-documentation);
- the In flight and Next lines for this slice.

The Rationale rows on the test shape and the vendor commit need no
home beyond this plan: the first is the test file's shape, the second
is `git log`.

## 6. How the knowledge lands

The build stage is `/rr-build docs/flight-listener-plan.md`, which
writes every comment and document row of sections 3 and 5 in the same
commit as the code and reconciles the code against this plan before the
build-stage message.

Specific to this unit:

- Commit 3 runs `go mod tidy` and `go mod vendor` after the first
  import, under `source .envrc`, and checks that `vendor/modules.txt`
  names grpc-go and protobuf at the versions of fact 2 or above and
  that no `duckdb-go-bindings/lib` archive was vendored
  (`third_party/AGENTS.md`). Facts 7 and 8 are confirmed in the vendored
  grpc-go before commits 4 and 6 cite them.
- If `SendHeader` on the Handshake fails arrow-go's client in commit 8,
  the executor stops and reports; fact 7 is then wrong and the owner
  decides between `SetHeader` with one sent message and a trailer.
- Commit 8 needs both local clusters up (`internal/AGENTS.md`, Tests).

## 7. Commits

1. `docs(plan): flight-listener-plan.md, the Flight SQL listener with Handshake and bearer auth` (this document)
2. `feat(config): listen.flight, default :40493, refused when empty`
3. `feat(flightsql): the package serves arrow-go's Flight SQL base server over TLS, grpc-go and protobuf vendored`
4. `feat(cmd): the Flight listener starts on listen.flight over the HTTPS certificates and drains under shutdownGrace`
5. `feat(flightsql): the auth interceptor verifies the bearer, admits anonymous on an insecure cluster and tags the logger`
6. `feat(flightsql): Handshake proves a Basic credential through Cluster.Authenticate and answers the access token header`
7. `feat(flightsql): one access line per RPC through a logging interceptor`
8. `test(flightsql): handshake, bearer, anonymous and refusal through arrow-go's client on both clusters`
9. `/doc-discipline all internal/flightsql cmd/qdb_rest internal/config internal/AGENTS.md`, its findings as further small commits
10. `/doc-discipline all check docs/flight-listener-plan.md`, its findings fixed before the next commit
11. `docs(log): flight-listener-plan.md deleted, facts moved to internal/flightsql/AGENTS.md, internal/AGENTS.md, cmd/qdb_rest/AGENTS.md and the comments`
12. Verify: push `sc-19567/rr-flight-listener`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

1. The owner's word that `qdb-documentation` is outside this project
   and tackled after the release removes the `tls_port` sentence from
   `docs/log.md`. The same sentence stands in `docs/brief.md`,
   Milestones, M5, and in ADR-0018's Consequences. Recommendation:
   commit 11 removes it from all three, as an in-place edit of a detail
   the owner reversed, with the one log entry naming the decision.
2. The breaker's retry-after is not carried on a Flight `UNAVAILABLE`,
   because gRPC has no `Retry-After` header. Recommendation: leave it
   out of the detail in this unit, and let the statement query slice
   decide whether the detail text carries it, since that slice maps the
   running query's errors.
