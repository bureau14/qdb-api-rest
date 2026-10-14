package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// Claims is a token's payload (brief, Authentication). It carries the
// reconnect material, the security handles, and the times, and the JWE
// seals and authenticates all of them. Timestamps are unix seconds, the
// JWT convention.
type Claims struct {
	Username   string `json:"username"`
	SecretKey  string `json:"secret_key"`
	SessionID  string `json:"sid"`
	Generation int64  `json:"gen"`
	Typ        string `json:"typ"`
	JTI        string `json:"jti"`
	AuthTime   int64  `json:"auth_time"`
	IssuedAt   int64  `json:"iat"`
	ExpiresAt  int64  `json:"exp"`
}

// LogValue keeps the secret key out of every log line.
func (c Claims) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("username", c.Username),
		slog.String("typ", c.Typ),
		slog.String("jti", c.JTI),
		slog.String("sid", c.SessionID))
}

// ErrInvalidToken covers every token that fails to parse, decrypt or
// decode. The reason is deliberately kept from clients.
var ErrInvalidToken = errors.New("invalid token")

// ErrTokenExpired means the token verified but its exp has passed.
var ErrTokenExpired = errors.New("token expired")

// maxTokenLength bounds what the verifier will even look at. A real
// token is a few hundred bytes.
const maxTokenLength = 4096

// header is the protected header this package mints. The verifier
// rejects any other shape.
type header struct {
	Alg string `json:"alg"`
	Enc string `json:"enc"`
	Kid string `json:"kid"`
}

// headerFor renders the protected header for one kid, deterministically.
func headerFor(kid string) string {
	return fmt.Sprintf(`{"alg":"dir","enc":"A256GCM","kid":%q}`, kid)
}

// parseHeader decodes a protected header strictly. It accepts only the
// three known fields and only our algorithms.
func parseHeader(raw []byte) (header, error) {
	// DisallowUnknownFields turns the parser into the allowlist. A zip or
	// crit field, or a foreign algorithm, fails here before it can act.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var h header
	if err := dec.Decode(&h); err != nil || h.Alg != "dir" || h.Enc != "A256GCM" {
		return header{}, ErrInvalidToken
	}
	return h, nil
}

// Tokens mints and verifies this gateway's tokens under the derived
// keychain. One verifier serves every typ profile (brief,
// Authentication).
type Tokens struct {
	keys       keychain
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time // injected (ADR-0006), so the tests move the clock
}

// New derives the keychain from the configured passphrases, once. With
// no passphrase configured it generates an ephemeral key and warns,
// because tokens then survive neither a restart nor a second instance.
func New(ctx context.Context, a config.Auth, now func() time.Time) (*Tokens, error) {
	if len(a.TokenSecrets) == 0 {
		observe.Logger(ctx).WarnContext(ctx,
			"no auth.token_secrets configured; tokens are sealed under an ephemeral key and die with this process")
	}
	kc, err := newKeychain(a)
	if err != nil {
		return nil, err
	}
	return &Tokens{keys: kc, accessTTL: a.AccessTTL, refreshTTL: a.RefreshTTL, now: now}, nil
}

// AccessTTL is how long an access token minted now stays valid.
func (t *Tokens) AccessTTL() time.Duration { return t.accessTTL }

// RefreshTTL is how long a refresh token minted now stays valid.
func (t *Tokens) RefreshTTL() time.Duration { return t.refreshTTL }

// Grant is what a login or a refresh hands the client: the access token
// that opens the data plane and the refresh token that renews it.
type Grant struct {
	Access  string
	Refresh string
}

// Issue seals the grant of a login: a fresh session id, auth_time and
// iat both now, the access token expiring one access lifetime from now
// and the refresh token one refresh lifetime from now.
func (t *Tokens) Issue(username, secretKey string) (Grant, error) {
	// The session id is fresh per login, because it is the security handle
	// of one login across every refresh that follows it.
	return t.grant(Claims{
		Username:  username,
		SecretKey: secretKey,
		SessionID: uuid.NewV7().String(),
		AuthTime:  t.now().Unix(),
	})
}

// grant seals two tokens from one base: each gets its typ, a fresh jti,
// iat now and exp at now plus its lifetime. Everything else is the
// base's.
func (t *Tokens) grant(base Claims) (Grant, error) {
	// The base carries what a login and a refresh share, and this function
	// owns what differs between the two tokens, so a change to the grant's
	// shape is made once instead of once per caller.
	//
	// 1. Read the clock once, so both tokens carry the same iat and their
	//    exp differ by the lifetimes alone.
	// 2. Seal the access token: typ access, exp one access lifetime out.
	// 3. Seal the refresh token: typ refresh, exp one refresh lifetime out.
	//    A previous refresh token stays valid until its own exp, because
	//    the server holds no state in which to retire it (ADR-0016).

	// 1. One clock read for both tokens.
	now := t.now().Unix()
	seal := func(typ string, ttl time.Duration) (string, error) {
		c := base
		c.Typ = typ
		c.JTI = uuid.NewV7().String()
		c.IssuedAt = now
		c.ExpiresAt = now + int64(ttl/time.Second)
		return t.Mint(c)
	}
	// 2. The access token.
	access, err := seal("access", t.accessTTL)
	if err != nil {
		return Grant{}, err
	}
	// 3. The refresh token. Nothing retires the one it replaces.
	refresh, err := seal("refresh", t.refreshTTL)
	if err != nil {
		return Grant{}, err
	}
	return Grant{Access: access, Refresh: refresh}, nil
}

// Mint seals claims into a compact JWE under the current key: protected
// header, empty encrypted-key segment (alg dir), random IV, ciphertext,
// tag, each base64url. The encoded header is the AAD (RFC 7516).
func (t *Tokens) Mint(c Claims) (string, error) {
	// The claims are the plaintext, secret key included, and all of it is
	// encrypted and authenticated.
	plaintext, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	// The encoded header doubles as AAD, so swapping it cannot re-point a
	// ciphertext at another key or mode.
	k := t.keys.mint
	protected := b64.EncodeToString([]byte(headerFor(k.kid)))
	// Each token gets a fresh random IV. At minting volume (one per login
	// or refresh) reuse is unreachable.
	iv := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	sealed := k.aead.Seal(nil, iv, plaintext, []byte(protected))
	// GCM appends its tag to the ciphertext, and JWE carries the tag as
	// its own segment.
	split := len(sealed) - k.aead.Overhead()
	// The compact form is header.key.iv.ciphertext.tag, and the key
	// segment is empty under alg dir.
	return strings.Join([]string{
		protected,
		"",
		b64.EncodeToString(iv),
		b64.EncodeToString(sealed[:split]),
		b64.EncodeToString(sealed[split:]),
	}, "."), nil
}

// Verify opens a compact JWE and returns its claims. Any malformed,
// unknown-key or tampered token is ErrInvalidToken. A genuine token past
// its exp is ErrTokenExpired.
func (t *Tokens) Verify(token string) (Claims, error) {
	// Refuse absurd input first. A real token is a few hundred bytes.
	if len(token) > maxTokenLength {
		return Claims{}, ErrInvalidToken
	}
	// A token has five dot-separated segments, and the second (the
	// encrypted key) is empty under dir. Every failure from here down is
	// the same ErrInvalidToken, because distinguishing them hands an
	// attacker an oracle.
	parts := strings.Split(token, ".")
	if len(parts) != 5 || parts[1] != "" {
		return Claims{}, ErrInvalidToken
	}
	raw, err := b64.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	h, err := parseHeader(raw)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	// The kid picks the key. An unknown kid means rotated out or foreign.
	k, ok := t.keys.verify[h.Kid]
	if !ok {
		return Claims{}, ErrInvalidToken
	}
	// Check the nonce length, because GCM panics on a wrong size and this
	// is attacker input.
	iv, err := b64.DecodeString(parts[2])
	if err != nil || len(iv) != k.aead.NonceSize() {
		return Claims{}, ErrInvalidToken
	}
	ciphertext, err := b64.DecodeString(parts[3])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	tag, err := b64.DecodeString(parts[4])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	// One Open authenticates header (AAD), ciphertext and tag together,
	// and nothing decrypted leaves a failed Open.
	plaintext, err := k.aead.Open(nil, iv, append(ciphertext, tag...), []byte(parts[0]))
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	// The claims decode is lenient, because a one-release-behind instance
	// must accept a newer token mid-upgrade.
	var c Claims
	if err := json.Unmarshal(plaintext, &c); err != nil {
		return Claims{}, ErrInvalidToken
	}
	// Time is checked last, on an authentic token only, because an expiry
	// answer proves the token was once genuine.
	if t.now().Unix() >= c.ExpiresAt {
		return Claims{}, ErrTokenExpired
	}
	return c, nil
}
