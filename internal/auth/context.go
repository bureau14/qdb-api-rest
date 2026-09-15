package auth

import "context"

// Tokens travels in the context next to the logger and the cluster: main
// places it in the process context, the server hands that context to
// every request, and handlers read it from there.
type tokensKey struct{}

// WithTokens returns ctx carrying t.
func WithTokens(ctx context.Context, t *Tokens) context.Context {
	return context.WithValue(ctx, tokensKey{}, t)
}

// TokensFrom returns the Tokens carried by ctx and panics without one: a
// fresh context mid-call-chain is a programming error.
func TokensFrom(ctx context.Context) *Tokens {
	t, ok := ctx.Value(tokensKey{}).(*Tokens)
	if !ok {
		panic("auth: no tokens in context")
	}
	return t
}

// Claims travel in the request context once the bearer middleware has
// verified a token: the edge places them, the handler reads them and
// builds the cluster user from them.
type claimsKey struct{}

// WithClaims returns ctx carrying c.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// ClaimsFrom returns the Claims carried by ctx and panics without them:
// a handler behind the bearer middleware always finds them, so their
// absence is a route registered without it.
func ClaimsFrom(ctx context.Context) Claims {
	c, ok := ctx.Value(claimsKey{}).(Claims)
	if !ok {
		panic("auth: no claims in context")
	}
	return c
}
