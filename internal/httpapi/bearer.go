package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// bearerToken extracts the token of an "Authorization: Bearer <token>"
// header: the scheme case-insensitive (RFC 9110), one token, nothing
// else. The second result says whether a header was present at all,
// which decides the challenge on failure.
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

// unauthorized answers 401 with the RFC 6750 challenge: a bare Bearer
// when no token was presented, error="invalid_token" when one was and
// failed. detail says which failure, since a genuine token's expiry is
// no oracle and the verifier already tells the two apart.
func unauthorized(w http.ResponseWriter, present bool, detail string) {
	challenge := "Bearer"
	if present {
		challenge = `Bearer error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	writeProblem(w, http.StatusUnauthorized, detail)
}

// requireBearer authenticates one route: the token is verified with the
// keychain from the ctx, must be an access token, and its claims ride
// the ctx into next together with the user and session log attributes.
// Applied per route, never to the mux: the probes stay unauthenticated.
func requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// No header at all is a bare challenge; a malformed one counts
		// as a presented, invalid token.
		token, present := bearerToken(r)
		if token == "" {
			unauthorized(w, present, "missing bearer token")
			return
		}
		// Verify distinguishes a tampered or foreign token from a genuine
		// expired one; both are 401, the detail differs.
		c, err := auth.TokensFrom(ctx).Verify(token)
		switch {
		case errors.Is(err, auth.ErrTokenExpired):
			unauthorized(w, true, "token expired")
			return
		case err != nil:
			unauthorized(w, true, "invalid token")
			return
		}
		// A refresh token is a credential for the refresh endpoint only,
		// never for the data plane.
		if c.Typ != "access" {
			unauthorized(w, true, "not an access token")
			return
		}
		// The edge enriches: claims for the handler, attributes for every
		// line logged below.
		ctx = auth.WithClaims(ctx, c)
		ctx = observe.WithAttrs(ctx, observe.KeyUser, c.Username, observe.KeySession, c.SessionID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
