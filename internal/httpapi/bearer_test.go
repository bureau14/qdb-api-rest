package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/config"
)

// epoch is the fixed clock every token in these tests is judged against.
var epoch = time.Unix(1_700_000_000, 0)

// tokensAt builds a keychain judging expiry at now: the default auth
// config, so an ephemeral key and no argon2id cost.
func tokensAt(t *testing.T, now time.Time) *auth.Tokens {
	t.Helper()
	tk, err := auth.New(observeContext(), config.Default().Auth, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

// mint seals a token of kind typ for user, expiring at exp.
func mint(t *testing.T, tk *auth.Tokens, typ, user string, exp time.Time) string {
	t.Helper()
	token, err := tk.Mint(auth.Claims{Username: user, Typ: typ, SessionID: "sid-1", ExpiresAt: exp.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// challenge runs one request with the given Authorization header through
// requireBearer over a handler that records the claims it received.
func challenge(t *testing.T, tk *auth.Tokens, authorization string) (*httptest.ResponseRecorder, *auth.Claims) {
	t.Helper()
	var seen *auth.Claims
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := auth.ClaimsFrom(r.Context())
		seen = &c
		w.WriteHeader(http.StatusNoContent)
	})
	ctx := auth.WithTokens(observeContext(), tk)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp := httptest.NewRecorder()
	requireBearer(inner).ServeHTTP(resp, req)
	return resp, seen
}

// problemDetail decodes the detail of a problem body.
func problemDetail(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	if ct := resp.Header().Get("Content-Type"); ct != problemContentType {
		t.Fatalf("Content-Type = %q, want %s", ct, problemContentType)
	}
	var p problem
	if err := json.Unmarshal(resp.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p.Detail
}

// TestBearerAccessTokenPasses: a minted access token reaches the handler
// with its claims, the scheme case-insensitive.
func TestBearerAccessTokenPasses(t *testing.T) {
	tk := tokensAt(t, epoch)
	token := mint(t, tk, "access", "alice", epoch.Add(time.Minute))
	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		resp, seen := challenge(t, tk, scheme+" "+token)
		if resp.Code != http.StatusNoContent || seen == nil || seen.Username != "alice" {
			t.Fatalf("%s: status %d, claims %+v", scheme, resp.Code, seen)
		}
	}
}

// TestBearerRejects: every failure is 401 with the RFC 6750 challenge --
// a bare Bearer when nothing was presented, error="invalid_token" when
// a token was -- and a detail that names the failure.
func TestBearerRejects(t *testing.T) {
	tk := tokensAt(t, epoch)
	cases := map[string]struct {
		authorization string
		challenge     string
		detail        string
	}{
		"missing":      {"", "Bearer", "missing bearer token"},
		"wrong scheme": {"Basic abc", `Bearer error="invalid_token"`, "malformed bearer credentials"},
		"garbage":      {"Bearer not.a.token", `Bearer error="invalid_token"`, "invalid token"},
		"refresh":      {"Bearer " + mint(t, tk, "refresh", "alice", epoch.Add(time.Hour)), `Bearer error="invalid_token"`, "not an access token"},
		"expired":      {"Bearer " + mint(t, tk, "access", "alice", epoch.Add(-time.Second)), `Bearer error="invalid_token"`, "token expired"},
	}
	for name, tc := range cases {
		resp, seen := challenge(t, tk, tc.authorization)
		if resp.Code != http.StatusUnauthorized || seen != nil {
			t.Errorf("%s: status %d, claims %+v", name, resp.Code, seen)
		}
		if got := resp.Header().Get("WWW-Authenticate"); got != tc.challenge {
			t.Errorf("%s: WWW-Authenticate = %q, want %q", name, got, tc.challenge)
		}
		if got := problemDetail(t, resp); !strings.Contains(got, tc.detail) {
			t.Errorf("%s: detail = %q, want %q", name, got, tc.detail)
		}
	}
}
