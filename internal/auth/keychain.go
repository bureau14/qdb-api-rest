// Package auth mints and verifies the gateway's tokens (ADR-0005):
// compact JWE, dir + A256GCM, under keys derived from the configured
// passphrases. This binary is the token's only producer and consumer;
// clients treat tokens as opaque strings.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"

	"github.com/bureau14/qdb-api-rest/internal/config"
)

// salt fills argon2id's required salt parameter, a constant because keys
// derive from config alone and there is no per-user storage for a random
// one. It domain-separates our derivations from any other argon2id user,
// and bumping its version suffix re-derives every key and kid, which
// behaves as a key rotation (ADR-0005).
var salt = []byte("qdb-rest/token-secrets/v1")

// b64 is the JOSE alphabet: base64url, unpadded, and canonical -- the
// default decoder ignores non-zero trailing padding bits, which would
// let one token have several byte spellings.
var b64 = base64.RawURLEncoding.Strict()

// key is one keychain entry: the kid a token carries and the AEAD its
// payload is sealed with, derived independently from one passphrase.
type key struct {
	kid  string
	aead cipher.AEAD
}

// expand derives one purpose's bytes from stretched key material; the
// info string is the domain separator (ADR-0005: the encryption key and
// the kid never derive from each other).
func expand(prk []byte, info string, n int) ([]byte, error) {
	return hkdf.Expand(sha256.New, prk, info, n)
}

// keyFrom splits stretched key material into a keychain entry: 32 bytes
// of AES-256-GCM key under "enc", 8 bytes of kid under "kid".
func keyFrom(prk []byte) (key, error) {
	enc, err := expand(prk, "enc", 32)
	if err != nil {
		return key{}, err
	}
	kid, err := expand(prk, "kid", 8)
	if err != nil {
		return key{}, err
	}
	block, err := aes.NewCipher(enc)
	if err != nil {
		return key{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return key{}, err
	}
	return key{kid: b64.EncodeToString(kid), aead: aead}, nil
}

// derive stretches one passphrase at the configured argon2id cost, the
// toll one attacker guess pays (~100ms at the default cost).
func derive(passphrase string, cost config.Argon2id) (key, error) {
	prk := argon2.IDKey([]byte(passphrase), salt,
		uint32(cost.Time), uint32(cost.MemoryMiB)*1024, uint8(cost.Parallelism), 32)
	return keyFrom(prk)
}

// keychain holds the derived keys: mint is the first configured
// passphrase's key, verify accepts every configured passphrase's, by
// kid (rolling keys, ADR-0005).
type keychain struct {
	mint   key
	verify map[string]key
}

// ephemeral is a single-key keychain from random material, the fallback
// when no passphrase is configured: tokens then survive neither a
// restart nor a second instance. Random material needs no argon2id
// stretching and takes the same HKDF split.
func ephemeral() (keychain, error) {
	prk := make([]byte, 32)
	if _, err := rand.Read(prk); err != nil {
		return keychain{}, err
	}
	k, err := keyFrom(prk)
	if err != nil {
		return keychain{}, err
	}
	return keychain{mint: k, verify: map[string]key{k.kid: k}}, nil
}

// newKeychain derives every configured passphrase once, first entry
// minting, or falls back to an ephemeral key.
func newKeychain(a config.Auth) (keychain, error) {
	if len(a.TokenSecrets) == 0 {
		return ephemeral()
	}
	kc := keychain{verify: map[string]key{}}
	for i, passphrase := range a.TokenSecrets {
		k, err := derive(passphrase, a.Argon2id)
		if err != nil {
			return keychain{}, fmt.Errorf("auth.token_secrets[%d]: %w", i, err)
		}
		if i == 0 {
			kc.mint = k
		}
		kc.verify[k.kid] = k
	}
	return kc, nil
}
