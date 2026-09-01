package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
	"uuid"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// legacyTokenTTL is the validity of tokens minted by the legacy login
// endpoint, part of the compatibility contract (brief, POST /api/login).
const legacyTokenTTL = 12 * time.Hour

// loginRequest is the legacy body: the fields of a QuasarDB user
// private-key file. Empty or absent username is an anonymous login.
type loginRequest struct {
	Username  string `json:"username"`
	SecretKey string `json:"secret_key"`
}

// writeJSON writes v as the response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleLegacyLogin mints a 12h token for the presented credentials.
// Nothing dials: login finds or creates no session (ADR-0003), and bad
// credentials surface on the first query, exactly as the old server
// behaved on insecure clusters.
func handleLegacyLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})
		return
	}
	now := time.Now()
	// A fresh sid and jti per login; auth_time equals iat because this
	// is an original login, never a refresh.
	token, err := auth.TokensFrom(ctx).Mint(auth.Claims{
		Username:  req.Username,
		SecretKey: req.SecretKey,
		SessionID: uuid.NewV7().String(),
		// Legacy tokens are long-lived access tokens: typ access, the
		// contract's 12h exp, and no refresh counterpart.
		Typ:       "access",
		JTI:       uuid.NewV7().String(),
		AuthTime:  now.Unix(),
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(legacyTokenTTL).Unix(),
	})
	if err != nil {
		observe.Logger(ctx).ErrorContext(ctx, "minting a login token failed", observe.Err(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// registerAuthRoutes serves the legacy login at its historical
// unversioned path and its /api/v1 alias, the same handler and never a
// redirect (brief, Compatibility contract).
func registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/login", handleLegacyLogin)
	mux.HandleFunc("POST /api/v1/login", handleLegacyLogin)
}
