package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// bearerToken extracts the token of an "Authorization: Bearer <token>"
// header. The scheme is matched case-insensitively (RFC 9110), and the
// header must carry one token and nothing else. The second result says
// whether a header was present at all, which decides the challenge on
// failure.
func bearerToken(r *http.Request) (token string, present bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	scheme, rest, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", true
	}
	token = strings.TrimSpace(rest)
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", true
	}
	return token, true
}

// unauthorized answers 401 with the RFC 6750 challenge. The challenge
// is a bare Bearer when no token was presented and error="invalid_token"
// when one was presented and failed. detail names the failure. Naming
// an expired genuine token leaks nothing, because the verifier already
// tells the two apart.
func unauthorized(w http.ResponseWriter, present bool, detail string) {
	challenge := "Bearer"
	if present {
		challenge = `Bearer error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	writeProblem(w, http.StatusUnauthorized, detail)
}

// requireBearer authenticates one route. It verifies the token with the
// keychain from the ctx and requires an access token, and the token's
// claims ride the ctx into next together with the user and session log
// attributes. It is applied per route and never to the mux, because the
// probes stay unauthenticated.
func requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// No header at all gets a bare challenge. A malformed header counts
		// as a presented, invalid token.
		token, present := bearerToken(r)
		switch {
		case !present:
			unauthorized(w, false, "missing bearer token")
			return
		case token == "":
			unauthorized(w, true, "malformed bearer credentials")
			return
		}
		// Verify tells a tampered or foreign token from a genuine expired
		// one. Both answer 401, and the detail names which.
		c, err := auth.TokensFrom(ctx).Verify(token)
		switch {
		case errors.Is(err, auth.ErrTokenExpired):
			unauthorized(w, true, "token expired")
			return
		case err != nil:
			unauthorized(w, true, "invalid token")
			return
		}
		// A refresh token is a credential for the refresh endpoint only. It
		// never opens the data plane.
		if c.Typ != "access" {
			unauthorized(w, true, "not an access token")
			return
		}
		// The edge enriches the ctx. The claims are for the handler, and the
		// attributes are for every line logged below.
		ctx = auth.WithClaims(ctx, c)
		ctx = observe.WithAttrs(ctx, observe.KeyUser, c.Username, observe.KeySession, c.SessionID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
