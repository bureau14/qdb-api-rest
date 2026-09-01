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

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// Claims is a token's payload (brief, Authentication): the reconnect
// material, the security handles, and the times, all sealed and
// authenticated by the JWE. Timestamps are unix seconds, the JWT
// convention.
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
// decode; the reason is deliberately not surfaced to clients.
var ErrInvalidToken = errors.New("invalid token")

// ErrTokenExpired: the token verified but its exp has passed.
var ErrTokenExpired = errors.New("token expired")

// maxTokenLength bounds what the verifier will even look at; a real
// token is a few hundred bytes.
const maxTokenLength = 4096

// header is the one protected header this package mints; the verifier
// rejects any other shape (ADR-0005).
type header struct {
	Alg string `json:"alg"`
	Enc string `json:"enc"`
	Kid string `json:"kid"`
}

// headerFor renders the protected header for one kid, deterministically.
func headerFor(kid string) string {
	return fmt.Sprintf(`{"alg":"dir","enc":"A256GCM","kid":%q}`, kid)
}

// parseHeader decodes a protected header strictly: exactly the three
// known fields, exactly our algorithms.
func parseHeader(raw []byte) (header, error) {
	// DisallowUnknownFields makes the allowlist the whole parser: any
	// extra parameter -- zip, crit, an algorithm we do not speak -- is
	// an error before any of it can take effect.
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
	keys keychain
	now  func() time.Time
}

// New derives the keychain from the configured passphrases, once. With
// no passphrase configured it generates an ephemeral key and warns:
// tokens then survive neither a restart nor a second instance
// (ADR-0005).
func New(ctx context.Context, a config.Auth, now func() time.Time) (*Tokens, error) {
	if len(a.TokenSecrets) == 0 {
		observe.Logger(ctx).WarnContext(ctx,
			"no auth.token_secrets configured; tokens are sealed under an ephemeral key and die with this process")
	}
	kc, err := newKeychain(a)
	if err != nil {
		return nil, err
	}
	return &Tokens{keys: kc, now: now}, nil
}

// Mint seals claims into a compact JWE under the current key: protected
// header, empty encrypted-key segment (alg dir), random IV, ciphertext,
// tag, each base64url. The encoded header is the AAD (RFC 7516).
func (t *Tokens) Mint(c Claims) (string, error) {
	// The claims are the JWE plaintext: everything in them, secret key
	// included, ends up encrypted and authenticated.
	plaintext, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	// The encoded protected header doubles as the AAD, so the header is
	// authenticated too: nobody can re-point an existing ciphertext at
	// another key or mode by swapping the header.
	k := t.keys.mint
	protected := b64.EncodeToString([]byte(headerFor(k.kid)))
	// One fresh random IV per token. GCM's safety rests on never
	// reusing one under the same key; at token-minting volume (one per
	// login or refresh) the random-collision bound is unreachable
	// within any rotation period (ADR-0005).
	iv := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	sealed := k.aead.Seal(nil, iv, plaintext, []byte(protected))
	// GCM appends its tag to the ciphertext; JWE carries the tag as its
	// own segment.
	split := len(sealed) - k.aead.Overhead()
	// Compact serialization: header.key.iv.ciphertext.tag. The key
	// segment stays empty -- alg dir means the derived key is used
	// directly, nothing about it travels in the token.
	return strings.Join([]string{
		protected,
		"",
		b64.EncodeToString(iv),
		b64.EncodeToString(sealed[:split]),
		b64.EncodeToString(sealed[split:]),
	}, "."), nil
}

// Verify opens a compact JWE and returns its claims. Any malformed,
// unknown-key or tampered token is ErrInvalidToken; a genuine token past
// its exp is ErrTokenExpired.
func (t *Tokens) Verify(token string) (Claims, error) {
	// Refuse absurd input before doing any work on it; a real token is
	// a few hundred bytes.
	if len(token) > maxTokenLength {
		return Claims{}, ErrInvalidToken
	}
	// A compact JWE is exactly five dot-separated segments, and under
	// alg dir the second -- the encrypted key -- is empty. Every
	// failure from here down is the same ErrInvalidToken: a verifier
	// that distinguishes parse errors from decryption errors hands an
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
	// The header's kid picks the key; a kid the keychain does not hold
	// means a rotated-out or foreign key, and the token is dead.
	k, ok := t.keys.verify[h.Kid]
	if !ok {
		return Claims{}, ErrInvalidToken
	}
	// The nonce length is checked, not assumed: GCM panics on a
	// wrong-size nonce, and this is attacker-supplied input.
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
	// One Open authenticates everything at once: the header (as AAD),
	// the ciphertext and the tag. A change to any segment fails here,
	// and nothing decrypted is ever released from a failed Open.
	plaintext, err := k.aead.Open(nil, iv, append(ciphertext, tag...), []byte(parts[0]))
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	// The claims decode leniently, on purpose: during a rolling upgrade
	// an instance one release behind must accept a newer token, so an
	// unknown claim is ignored, never refused.
	var c Claims
	if err := json.Unmarshal(plaintext, &c); err != nil {
		return Claims{}, ErrInvalidToken
	}
	// Time is judged last, on an authentic token only, so an expired
	// answer proves the token was once genuine.
	if t.now().Unix() >= c.ExpiresAt {
		return Claims{}, ErrTokenExpired
	}
	return c, nil
}
