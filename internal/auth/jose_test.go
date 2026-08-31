package auth

// go-jose is the independent JWE implementation these tests validate
// against (ADR-0005): a self-consistent-but-wrong encoder passes its own
// roundtrip and fails only against another implementation. Test-only
// dependency; it never links into the binary.
//
// The derivation pipeline is recomputed here from the raw primitives --
// same salt, costs and HKDF split as keychain.go -- so these tests pin
// the derivation as a protocol, not as whatever the implementation does.

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/crypto/argon2"
	"pgregory.net/rapid"
)

// stretch recomputes argon2id at testCost over the production salt.
func stretch(passphrase string) []byte {
	return argon2.IDKey([]byte(passphrase), []byte("qdb-rest/token-secrets/v1"), 1, 1024, 1, 32)
}

// split recomputes one HKDF purpose from stretched material.
func split(t failer, prk []byte, info string, n int) []byte {
	t.Helper()
	out, err := hkdf.Expand(sha256.New, prk, info, n)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Every token we mint is a JWE go-jose decrypts, under the key the
// documented derivation produces, carrying the derived kid.
func TestJoseDecryptsOurTokens(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		secrets := secretsGen().Draw(rt, "secrets")
		tk := tokensFor(rt, secrets)
		c := claimsGen(rt)
		token, err := tk.Mint(c)
		if err != nil {
			rt.Fatal(err)
		}
		jwe, err := jose.ParseEncrypted(token,
			[]jose.KeyAlgorithm{jose.DIRECT}, []jose.ContentEncryption{jose.A256GCM})
		if err != nil {
			rt.Fatalf("go-jose rejects the serialization: %v", err)
		}
		prk := stretch(secrets[0])
		if want := b64.EncodeToString(split(rt, prk, "kid", 8)); jwe.Header.KeyID != want {
			rt.Fatalf("kid = %q, want %q", jwe.Header.KeyID, want)
		}
		plaintext, err := jwe.Decrypt(split(rt, prk, "enc", 32))
		if err != nil {
			rt.Fatalf("go-jose cannot decrypt: %v", err)
		}
		var got Claims
		if err := json.Unmarshal(plaintext, &got); err != nil {
			rt.Fatal(err)
		}
		if !reflect.DeepEqual(got, c) {
			rt.Fatalf("got %+v, want %+v", got, c)
		}
	})
}

// Every dir+A256GCM compact JWE go-jose mints under our derived key and
// kid passes our verifier.
func TestOurVerifierAcceptsJose(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		secrets := secretsGen().Draw(rt, "secrets")
		tk := tokensFor(rt, secrets)
		prk := stretch(secrets[0])
		encrypter, err := jose.NewEncrypter(jose.A256GCM, jose.Recipient{
			Algorithm: jose.DIRECT,
			Key:       split(rt, prk, "enc", 32),
			KeyID:     b64.EncodeToString(split(rt, prk, "kid", 8)),
		}, nil)
		if err != nil {
			rt.Fatal(err)
		}
		c := claimsGen(rt)
		plaintext, err := json.Marshal(c)
		if err != nil {
			rt.Fatal(err)
		}
		jwe, err := encrypter.Encrypt(plaintext)
		if err != nil {
			rt.Fatal(err)
		}
		token, err := jwe.CompactSerialize()
		if err != nil {
			rt.Fatal(err)
		}
		got, err := tk.Verify(token)
		if err != nil {
			rt.Fatalf("our verifier rejects go-jose's token: %v", err)
		}
		if !reflect.DeepEqual(got, c) {
			rt.Fatalf("got %+v, want %+v", got, c)
		}
	})
}
