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

// salt fills argon2id's required salt parameter. A constant is safe here
// because the classical purposes of a salt do not apply: keys derive
// from config alone, so there is no table of per-user records to
// decorrelate. What the constant still buys is domain separation -- a
// dictionary precomputed against any other argon2id user is useless
// against us -- and a version handle: bumping the suffix re-derives
// every key and kid, which behaves as a key rotation (ADR-0005).
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
	// Two independent expansions of the same stretched material: the
	// kid travels in every token header, public, so it must reveal
	// nothing about the encryption key -- neither derives from the
	// other, only from prk under its own info string.
	enc, err := expand(prk, "enc", 32)
	if err != nil {
		return key{}, err
	}
	// 8 bytes of kid is plenty: it only picks a keychain entry, it
	// never has to resist guessing.
	kid, err := expand(prk, "kid", 8)
	if err != nil {
		return key{}, err
	}
	// Wrap the 32 bytes as AES-256-GCM, the enc every token header
	// promises; past this point the raw key bytes are not kept.
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
	// Fresh randomness stands in for the stretched passphrase: 32
	// random bytes already carry full key entropy, so argon2id would
	// add nothing here.
	prk := make([]byte, 32)
	if _, err := rand.Read(prk); err != nil {
		return keychain{}, err
	}
	// From here the pipeline cannot tell the difference: the same HKDF
	// split yields the key and its kid, and the single entry both
	// mints and verifies.
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
	// One derivation per configured passphrase: the argon2id cost is
	// paid here, at startup, never per request.
	for i, passphrase := range a.TokenSecrets {
		k, err := derive(passphrase, a.Argon2id)
		if err != nil {
			return keychain{}, fmt.Errorf("auth.token_secrets[%d]: %w", i, err)
		}
		// The first entry is the current key and mints; every entry,
		// first included, verifies. Keyed by kid, so the verifier finds
		// the right key without trial decryption.
		if i == 0 {
			kc.mint = k
		}
		kc.verify[k.kid] = k
	}
	return kc, nil
}
