package httpapi

import (
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// loginRequest is the body: the fields of a QuasarDB user security file.
// An empty or absent username is an anonymous login.
type loginRequest struct {
	Username  string `json:"username"`
	SecretKey string `json:"secret_key"`
}

// LogValue keeps the secret key out of every log line.
func (l loginRequest) LogValue() slog.Value {
	return slog.GroupValue(slog.String("username", l.Username))
}

// user is the cluster user the request names; anonymous carries no
// secret, whatever the body said.
func (l loginRequest) user() qdb.User {
	if l.Username == "" {
		return qdb.User{}
	}
	return qdb.User{Username: l.Username, SecretKey: l.SecretKey}
}

// tokenResponse is RFC 6749's token response: the access token, its
// scheme and its validity in whole seconds.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// isJSON reports whether a Content-Type names application/json; a
// charset parameter is neither honored nor checked.
func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "application/json"
}

// handleLogin proves the presented credentials by one direct dial and
// answers with an access token. A refused dial is the caller's 401, an
// unreachable cluster 503; nothing is minted for credentials the cluster
// did not accept.
func handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// A non-JSON body is a clear 415 instead of a decode error.
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
	// The dial is the credential check; only its verdict reaches the
	// wire.
	u := req.user()
	if err := qdb.ClusterFrom(ctx).Authenticate(ctx, u); err != nil {
		writeClusterError(ctx, w, err, http.StatusUnauthorized)
		return
	}
	tk := auth.TokensFrom(ctx)
	token, err := tk.MintAccess(u.Username, u.SecretKey)
	if err != nil {
		observe.Logger(ctx).ErrorContext(ctx, "minting an access token failed", observe.Err(err))
		writeProblem(w, http.StatusInternalServerError, "minting the token failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tokenResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(tk.AccessTTL().Seconds()),
	})
}

// registerAuthRoutes serves the login, unauthenticated: it is where a
// caller, anonymous included, gets the token every other v2 route wants.
func registerAuthRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v2/auth/login", withCompression(http.HandlerFunc(handleLogin)))
}
