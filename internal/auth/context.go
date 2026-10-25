package auth

import "context"

// Tokens travels in the context next to the logger and the cluster.
// main places it in the process context, the server hands that context to
// every request, and handlers read it from there.
type tokensKey struct{}

// WithTokens returns a context that carries the tokens.
func WithTokens(ctx context.Context, t *Tokens) context.Context {
	return context.WithValue(ctx, tokensKey{}, t)
}

// TokensFrom returns the Tokens the context carries. It panics when there
// are none, because a fresh context in the middle of a call chain is a
// programming error.
func TokensFrom(ctx context.Context) *Tokens {
	t, ok := ctx.Value(tokensKey{}).(*Tokens)
	if !ok {
		panic("auth: no tokens in context")
	}
	return t
}

// Claims travel in the request context once the bearer middleware has
// verified a token. The edge places them, and the handler reads them
// and builds the cluster user from them.
type claimsKey struct{}

// WithClaims returns a context that carries the claims.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// ClaimsFrom returns the Claims the context carries, and panics when
// there are none. A handler behind the bearer middleware always finds
// them, so missing claims mean a route registered without the
// middleware.
func ClaimsFrom(ctx context.Context) Claims {
	c, ok := ctx.Value(claimsKey{}).(Claims)
	if !ok {
		panic("auth: no claims in context")
	}
	return c
}
