package v1

import (
	"net/http"
	"strings"
)

// unauthenticated is the old server's answer when a request carries no
// token at all.
const unauthenticated = "unauthenticated for invalid credentials"

// v1Tokens answers the tokens a request presents, in the order the old
// server tried them: the Authorization header, without one leading
// "Bearer ", then ?token=. An empty value is no token.
func v1Tokens(r *http.Request) []string {
	var tokens []string
	if h := r.Header.Get("Authorization"); h != "" {
		// The prefix is case-sensitive and optional, because the old server
		// stripped it and kept a bare header whole.
		t, _ := strings.CutPrefix(h, "Bearer ")
		tokens = append(tokens, t)
	}
	if q := r.URL.Query().Get("token"); q != "" {
		tokens = append(tokens, q)
	}
	return tokens
}
