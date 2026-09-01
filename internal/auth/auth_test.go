package auth

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// testCost keeps derivation around a millisecond; the production cost is
// an operator knob, not what these properties pin.
var testCost = config.Argon2id{Time: 1, MemoryMiB: 1, Parallelism: 1}

// epoch is the tests' fixed clock.
var epoch = time.Unix(1_700_000_000, 0)

// ctx carries a discarding logger, the only thing New logs to.
func ctx() context.Context {
	return observe.WithLogger(context.Background(), slog.New(slog.DiscardHandler))
}

// failer is the slice of testing.TB that tokensFor needs, so *testing.T
// and *rapid.T both fit.
type failer interface {
	Helper()
	Fatal(...any)
}

// tokensFor builds a verifier over the given passphrases at test cost.
func tokensFor(t failer, secrets []string) *Tokens {
	t.Helper()
	tk, err := New(ctx(), config.Auth{TokenSecrets: secrets, Argon2id: testCost}, func() time.Time { return epoch })
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

// secretsGen draws 1..3 distinct non-empty passphrases.
func secretsGen() *rapid.Generator[[]string] {
	return rapid.SliceOfNDistinct(rapid.StringN(1, 64, -1), 1, 3, rapid.ID)
}

// claimsGen draws claims over the full string space, expiring in the
// future relative to epoch.
func claimsGen(rt *rapid.T) Claims {
	return Claims{
		Username:   rapid.String().Draw(rt, "username"),
		SecretKey:  rapid.String().Draw(rt, "secret_key"),
		SessionID:  rapid.String().Draw(rt, "sid"),
		Generation: rapid.Int64().Draw(rt, "gen"),
		Typ:        rapid.SampledFrom([]string{"access", "refresh"}).Draw(rt, "typ"),
		JTI:        rapid.String().Draw(rt, "jti"),
		AuthTime:   rapid.Int64Range(0, epoch.Unix()).Draw(rt, "auth_time"),
		IssuedAt:   epoch.Unix(),
		ExpiresAt:  epoch.Unix() + rapid.Int64Range(1, 43200).Draw(rt, "ttl"),
	}
}

// Whatever goes in comes out, under any passphrase list.
func TestMintVerifyRoundtrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tk := tokensFor(rt, secretsGen().Draw(rt, "secrets"))
		c := claimsGen(rt)
		token, err := tk.Mint(c)
		if err != nil {
			rt.Fatal(err)
		}
		got, err := tk.Verify(token)
		if err != nil {
			rt.Fatal(err)
		}
		if !reflect.DeepEqual(got, c) {
			rt.Fatalf("got %+v, want %+v", got, c)
		}
	})
}

// A genuine token past its exp is expired, never invalid.
func TestExpiredRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tk := tokensFor(rt, secretsGen().Draw(rt, "secrets"))
		c := claimsGen(rt)
		c.ExpiresAt = epoch.Unix() - rapid.Int64Range(0, 43200).Draw(rt, "past")
		token, err := tk.Mint(c)
		if err != nil {
			rt.Fatal(err)
		}
		if _, err := tk.Verify(token); !errors.Is(err, ErrTokenExpired) {
			rt.Fatalf("want ErrTokenExpired, got %v", err)
		}
	})
}

// Rotation: a token minted under the old passphrase verifies while the
// old passphrase is still listed, and stops the moment it is dropped;
// re-minting under the new list propagates the rotation.
func TestRotationContinuity(t *testing.T) {
	old := tokensFor(t, []string{"old passphrase"})
	c := Claims{Username: "alice", SecretKey: "sk", Typ: "access", ExpiresAt: epoch.Unix() + 60}
	token, err := old.Mint(c)
	if err != nil {
		t.Fatal(err)
	}
	rotated := tokensFor(t, []string{"new passphrase", "old passphrase"})
	if _, err := rotated.Verify(token); err != nil {
		t.Fatalf("rotated keychain rejects the old token: %v", err)
	}
	reminted, err := rotated.Mint(c)
	if err != nil {
		t.Fatal(err)
	}
	final := tokensFor(t, []string{"new passphrase"})
	if _, err := final.Verify(reminted); err != nil {
		t.Fatalf("final keychain rejects the re-minted token: %v", err)
	}
	if _, err := final.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("dropped passphrase still verifies: %v", err)
	}
}

// Any single-character change to a token stops it from verifying; the
// verifier returns an error and never panics.
func TestTamperNeverVerifies(t *testing.T) {
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_."
	rapid.Check(t, func(rt *rapid.T) {
		tk := tokensFor(rt, secretsGen().Draw(rt, "secrets"))
		c := claimsGen(rt)
		token, err := tk.Mint(c)
		if err != nil {
			rt.Fatal(err)
		}
		i := rapid.IntRange(0, len(token)-1).Draw(rt, "index")
		r := alphabet[rapid.IntRange(0, len(alphabet)-1).Draw(rt, "replacement")]
		if token[i] == r {
			rt.Skip("replacement equals original")
		}
		tampered := token[:i] + string(r) + token[i+1:]
		if _, err := tk.Verify(tampered); err == nil {
			rt.Fatalf("tampered token verified: %q -> %q", token, tampered)
		}
	})
}

// Garbage never panics the verifier.
func TestVerifyNeverPanics(t *testing.T) {
	tk := tokensFor(t, []string{"passphrase"})
	rapid.Check(t, func(rt *rapid.T) {
		garbage := rapid.String().Draw(rt, "garbage")
		if rapid.Bool().Draw(rt, "dotted") {
			garbage = strings.Join([]string{garbage, "", garbage, garbage, garbage}, ".")
		}
		if _, err := tk.Verify(garbage); err == nil {
			rt.Fatalf("garbage verified: %q", garbage)
		}
	})
}

// Two ephemeral keychains never accept each other's tokens.
func TestEphemeralIsolated(t *testing.T) {
	a := tokensFor(t, nil)
	b := tokensFor(t, nil)
	token, err := a.Mint(Claims{ExpiresAt: epoch.Unix() + 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Verify(token); err != nil {
		t.Fatalf("own token rejected: %v", err)
	}
	if _, err := b.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("foreign ephemeral token accepted: %v", err)
	}
}

// New refuses what derivation cannot use, naming the config key.
func TestBadConfigRefused(t *testing.T) {
	for name, a := range map[string]config.Auth{
		"empty passphrase":     {TokenSecrets: []string{""}, Argon2id: testCost},
		"duplicate passphrase": {TokenSecrets: []string{"a", "a"}, Argon2id: testCost},
		"zero time":            {TokenSecrets: []string{"a"}, Argon2id: config.Argon2id{Time: 0, MemoryMiB: 1, Parallelism: 1}},
		"zero memory":          {TokenSecrets: []string{"a"}, Argon2id: config.Argon2id{Time: 1, MemoryMiB: 0, Parallelism: 1}},
		"lanes overflow":       {TokenSecrets: []string{"a"}, Argon2id: config.Argon2id{Time: 1, MemoryMiB: 1, Parallelism: 256}},
	} {
		if _, err := New(ctx(), a, func() time.Time { return epoch }); err == nil || !strings.Contains(err.Error(), "auth.") {
			t.Errorf("%s: want a refusal naming the key, got %v", name, err)
		}
	}
}

// Changing any argon2id cost re-derives key and kid: a cost bump behaves
// as a key rotation (ADR-0005).
func TestCostChangeRollsKeys(t *testing.T) {
	bumped := config.Argon2id{Time: 2, MemoryMiB: 1, Parallelism: 1}
	before, err := derive("passphrase", testCost)
	if err != nil {
		t.Fatal(err)
	}
	after, err := derive("passphrase", bumped)
	if err != nil {
		t.Fatal(err)
	}
	if before.kid == after.kid {
		t.Fatal("cost change kept the kid")
	}
}
