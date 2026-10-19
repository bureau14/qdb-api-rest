package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/build"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// loginRequest is the body, which carries the fields of a QuasarDB user
// security file. An empty or absent username is an anonymous login.
type loginRequest struct {
	Username  string `json:"username"`
	SecretKey string `json:"secret_key"`
}

// LogValue keeps the secret key out of every log line.
func (l loginRequest) LogValue() slog.Value {
	return slog.GroupValue(slog.String("username", l.Username))
}

// user is the cluster user the request names. An anonymous user carries
// no secret, whatever the body said.
func (l loginRequest) user() qdb.User {
	if l.Username == "" {
		return qdb.User{}
	}
	return qdb.User{Username: l.Username, SecretKey: l.SecretKey}
}

// tokenResponse is RFC 6749's token response. It carries the access
// token, its scheme, its validity in whole seconds, and the refresh
// token that renews it.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// writeTokenResponse answers 200 with the grant. expires_in is the
// access lifetime, because that is the token the client counts down. A
// client learns that its refresh token has expired from the refresh's
// 401, and does not count it (ADR-0016).
func writeTokenResponse(w http.ResponseWriter, tk *auth.Tokens, g auth.Grant) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tokenResponse{
		AccessToken:  g.Access,
		TokenType:    "Bearer",
		ExpiresIn:    int64(tk.AccessTTL().Seconds()),
		RefreshToken: g.Refresh,
	})
}

// isJSON reports whether a Content-Type names application/json. A
// charset parameter is neither honored nor checked.
func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "application/json"
}

// handleLogin proves the presented credentials by one direct dial and
// answers with a token grant. A refused dial is the caller's 401 and
// an unreachable cluster is 503. The handler mints nothing for
// credentials the cluster did not accept.
func handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// A non-JSON body gets a clear 415 instead of a decode error.
	if !isJSON(r.Header.Get("Content-Type")) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req loginRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	// The dial is the credential check. Only its verdict reaches the
	// wire. A refused credential answers 401 without a challenge before
	// writeClusterError runs, because the credentials were the body and
	// no bearer was in play.
	u := req.user()
	if err := qdb.ClusterFrom(ctx).Authenticate(ctx, u); err != nil {
		if errors.Is(err, qdb.ErrCredentialsRefused) && ctx.Err() == nil {
			writeProblem(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeClusterError(ctx, w, err, http.StatusUnauthorized)
		return
	}
	// The grant is written by the same function the refresh uses, so the
	// two routes answer one shape.
	tk := auth.TokensFrom(ctx)
	g, err := tk.Issue(u.Username, u.SecretKey)
	if err != nil {
		observe.Logger(ctx).ErrorContext(ctx, "minting the token grant failed", observe.Err(err))
		writeProblem(w, http.StatusInternalServerError, "minting the tokens failed")
		return
	}
	writeTokenResponse(w, tk, g)
}

// refreshRequest is the refresh body: the refresh token to exchange.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// handleRefresh exchanges a refresh token for a fresh grant without
// dialing the cluster. A token that fails to verify, is expired, or is
// not a refresh token is 401 with the bearer challenge, and the detail
// names which.
func handleRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The refresh is a pure function of the token, the keychain and the
	// clock. It touches neither the cluster, the budget nor the breaker,
	// so it has no 503, and clients may call it as often as they like
	// (ADR-0016). The checks run in the login's order:
	//
	// 1. Content type and body, as the login's: 415, 413, 400.
	// 2. Verify the token: expired and invalid are both 401 with the
	//    challenge, because a token was presented.
	// 3. Require a refresh token: an access token must not renew itself.
	// 4. Renew and write the grant. A mint failure is 500.

	// 1. The body is the login's shape with one field.
	if !isJSON(r.Header.Get("Content-Type")) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req refreshRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	// 2. Verify. The challenge is error="invalid_token" in both cases,
	//    because a token was presented and failed, which is what the
	//    bearer edge says of the same failures.
	tk := auth.TokensFrom(ctx)
	c, err := tk.Verify(req.RefreshToken)
	switch {
	case errors.Is(err, auth.ErrTokenExpired):
		unauthorized(w, true, "token expired")
		return
	case err != nil:
		unauthorized(w, true, "invalid token")
		return
	}
	// 3. Only a refresh token renews. An access token that could renew
	//    itself would live as long as a refresh token, and its lifetime
	//    is the bound on a leaked token (ADR-0016).
	if c.Typ != "refresh" {
		unauthorized(w, true, "not a refresh token")
		return
	}
	// 4. The grant is minted from the verified claims under the current
	//    key, which is how a key rotation reaches a client.
	g, err := tk.Renew(c)
	if err != nil {
		observe.Logger(ctx).ErrorContext(ctx, "minting the token grant failed", observe.Err(err))
		writeProblem(w, http.StatusInternalServerError, "minting the tokens failed")
		return
	}
	writeTokenResponse(w, tk, g)
}

// handleLogout answers 204. The server holds no session state, so there
// is nothing to forget. The verb exists so clients and the Flight layer
// have one, and the remedy for a leaked token is a key rotation
// (ADR-0016).
func handleLogout(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// sessionResponse is the caller's view of their session, assembled from
// the claims, the cluster this process fronts and the build, without a
// cluster round trip. The object is grouped by where each value comes
// from, so a reader of the support questions it serves ("who am I
// logged in as", "why am I unauthorized") sees the source of each
// answer. The secret is never echoed (ADR-0016).
type sessionResponse struct {
	Username string         `json:"username"`
	Cluster  sessionCluster `json:"cluster"`
	Token    sessionToken   `json:"token"`
	Server   sessionServer  `json:"server"`
}

// sessionCluster is the cluster the server fronts.
type sessionCluster struct {
	URI     string `json:"uri"`
	Secured bool   `json:"secured"`
}

// sessionToken is the access token's own claims. The times are unix
// seconds, as the claims carry them, so the handler converts nothing and
// the auth surface spells a time one way.
type sessionToken struct {
	Typ       string `json:"typ"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	JTI       string `json:"jti"`
	AuthTime  int64  `json:"auth_time"`
}

// sessionServer is this instance's build.
type sessionServer struct {
	Version string `json:"version"`
}

// handleSession answers the caller's session from the claims, the
// cluster it fronts, and the build version. It makes no cluster call,
// so it is cheap enough to poll. It reports what the token says, so a
// token whose credentials the cluster refuses still reads its session.
func handleSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := auth.ClaimsFrom(ctx)
	cl := qdb.ClusterFrom(ctx)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessionResponse{
		Username: c.Username,
		Cluster:  sessionCluster{URI: cl.URI(), Secured: cl.Secured()},
		Token:    sessionToken{Typ: c.Typ, IssuedAt: c.IssuedAt, ExpiresAt: c.ExpiresAt, JTI: c.JTI, AuthTime: c.AuthTime},
		Server:   sessionServer{Version: build.Version},
	})
}

// registerAuthRoutes serves the login and the refresh unauthenticated,
// because the login is where a caller, anonymous included, gets the
// token every other v2 route wants, and the refresh's credential is the
// refresh token in its body, which the bearer edge would refuse. The
// logout and the session sit behind the bearer edge, so only an access
// token reaches them.
func registerAuthRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v2/auth/login", withCompression(http.HandlerFunc(handleLogin)))
	mux.Handle("POST /api/v2/auth/refresh", withCompression(http.HandlerFunc(handleRefresh)))
	mux.Handle("POST /api/v2/auth/logout", requireBearer(http.HandlerFunc(handleLogout)))
	mux.Handle("GET /api/v2/session", requireBearer(withCompression(http.HandlerFunc(handleSession))))
}
