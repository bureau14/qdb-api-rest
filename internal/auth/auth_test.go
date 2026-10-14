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

// testCost keeps derivation around a millisecond. The production cost is
// an operator knob, and these properties do not pin it.
var testCost = config.Argon2id{Time: 1, MemoryMiB: 1, Parallelism: 1}

// epoch is the tests' fixed clock.
var epoch = time.Unix(1_700_000_000, 0)

// ctx carries a discarding logger, the only thing New logs to.
func ctx() context.Context {
	return observe.WithLogger(context.Background(), slog.New(slog.DiscardHandler))
}

// failer is the subset of testing.TB that tokensFor needs, so *testing.T
// and *rapid.T both fit.
type failer interface {
	Helper()
	Fatal(...any)
}

// tokensFor builds a verifier over the given passphrases at test cost,
// with the clock fixed at epoch.
func tokensFor(t failer, secrets []string) *Tokens {
	t.Helper()
	return tokensAt(t, secrets, func() time.Time { return epoch })
}

// tokensAt is tokensFor with a clock of the test's own. The clock moves
// through the injected field (ADR-0006), so a test that needs time to
// pass passes a closure over a variable it advances.
func tokensAt(t failer, secrets []string, now func() time.Time) *Tokens {
	t.Helper()
	tk, err := New(ctx(), config.Auth{TokenSecrets: secrets, AccessTTL: 15 * time.Minute, RefreshTTL: 14 * 24 * time.Hour, Argon2id: testCost}, now)
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
		// Mint under a drawn keychain. Verify must return the claims
		// bit-identical, whatever the strings contained.
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
// old passphrase is still listed, and stops the moment it is dropped.
// Re-minting under the new list propagates the rotation.
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

// Any single-character change to a token stops it from verifying. The
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
		// The alphabet stays within token characters, so every
		// mutation yields a plausible token, not a cheap parse reject.
		i := rapid.IntRange(0, len(token)-1).Draw(rt, "index")
		r := alphabet[rapid.IntRange(0, len(alphabet)-1).Draw(rt, "replacement")]
		// A replacement equal to the original proves nothing, so the test
		// skips it.
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
	// With nil secrets, each New draws its own random key.
	a := tokensFor(t, nil)
	b := tokensFor(t, nil)
	token, err := a.Mint(Claims{ExpiresAt: epoch.Unix() + 60})
	if err != nil {
		t.Fatal(err)
	}
	// a's token verifies at a and dies at b, which is the documented cost
	// of running unconfigured behind a load balancer.
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
		"empty passphrase":             {TokenSecrets: []string{""}, AccessTTL: time.Minute, RefreshTTL: time.Hour, Argon2id: testCost},
		"duplicate passphrase":         {TokenSecrets: []string{"a", "a"}, AccessTTL: time.Minute, RefreshTTL: time.Hour, Argon2id: testCost},
		"zero time":                    {TokenSecrets: []string{"a"}, AccessTTL: time.Minute, RefreshTTL: time.Hour, Argon2id: config.Argon2id{Time: 0, MemoryMiB: 1, Parallelism: 1}},
		"zero memory":                  {TokenSecrets: []string{"a"}, AccessTTL: time.Minute, RefreshTTL: time.Hour, Argon2id: config.Argon2id{Time: 1, MemoryMiB: 0, Parallelism: 1}},
		"lanes overflow":               {TokenSecrets: []string{"a"}, AccessTTL: time.Minute, RefreshTTL: time.Hour, Argon2id: config.Argon2id{Time: 1, MemoryMiB: 1, Parallelism: 256}},
		"zero access ttl":              {TokenSecrets: []string{"a"}, RefreshTTL: time.Hour, Argon2id: testCost},
		"zero refresh ttl":             {TokenSecrets: []string{"a"}, AccessTTL: time.Minute, Argon2id: testCost},
		"refresh ttl not above access": {TokenSecrets: []string{"a"}, AccessTTL: time.Minute, RefreshTTL: time.Minute, Argon2id: testCost},
	} {
		if _, err := New(ctx(), a, func() time.Time { return epoch }); err == nil || !strings.Contains(err.Error(), "auth.") {
			t.Errorf("%s: want a refusal naming the key, got %v", name, err)
		}
	}
}

// verified is the claims of a token that must verify.
func verified(t failer, tk *Tokens, token string) Claims {
	t.Helper()
	c, err := tk.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Issue fills in what a login fixes: the given user, both times set to
// now, exp one lifetime later per typ, and one sid shared by the two
// tokens. The jti is fresh per token, and the sid per call.
func TestIssue(t *testing.T) {
	tk := tokensFor(t, nil)
	issue := func() (Claims, Claims) {
		g, err := tk.Issue("alice", "s3cret")
		if err != nil {
			t.Fatal(err)
		}
		return verified(t, tk, g.Access), verified(t, tk, g.Refresh)
	}
	access, refresh := issue()
	again, _ := issue()
	if access.SessionID == "" || access.JTI == "" || refresh.JTI == "" || access.JTI == refresh.JTI || access.SessionID == again.SessionID || access.JTI == again.JTI {
		t.Fatalf("handles not fresh: %+v vs %+v vs %+v", access, refresh, again)
	}
	if access.SessionID != refresh.SessionID || access.AuthTime != refresh.AuthTime {
		t.Fatalf("the pair does not share its session: %+v vs %+v", access, refresh)
	}
	// Everything but the handles is determined by the login and the clock.
	access.SessionID, access.JTI, refresh.SessionID, refresh.JTI = "", "", "", ""
	want := Claims{
		Username: "alice", SecretKey: "s3cret", Typ: "access",
		AuthTime: epoch.Unix(), IssuedAt: epoch.Unix(), ExpiresAt: epoch.Add(tk.AccessTTL()).Unix(),
	}
	if !reflect.DeepEqual(access, want) {
		t.Fatalf("access: got %+v, want %+v", access, want)
	}
	want.Typ, want.ExpiresAt = "refresh", epoch.Add(tk.RefreshTTL()).Unix()
	if !reflect.DeepEqual(refresh, want) {
		t.Fatalf("refresh: got %+v, want %+v", refresh, want)
	}
}

// Renew slides the refresh lifetime: the new refresh token expires one
// refresh lifetime from the refresh, the grant keeps the login's user,
// secret, sid, gen and auth_time with fresh handles, and the old refresh
// token stays valid until its own exp, because the server holds no state
// in which to retire it (ADR-0016).
func TestRenewSlides(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// The clock is a closure over now, so the test issues at epoch and
		// then moves time forward before it renews.
		now := epoch
		tk := tokensAt(rt, nil, func() time.Time { return now })
		g, err := tk.Issue("alice", "s3cret")
		if err != nil {
			rt.Fatal(err)
		}
		// Issue mints gen as zero, so a drawn value stands in for a
		// revocation layer's generation, which Renew must carry through.
		old := verified(rt, tk, g.Refresh)
		old.Generation = rapid.Int64().Draw(rt, "gen")
		// Any instant under the refresh lifetime finds the old token alive.
		now = epoch.Add(time.Duration(rapid.Int64Range(0, int64(tk.RefreshTTL())-1).Draw(rt, "elapsed")))
		renewed, err := tk.Renew(old)
		if err != nil {
			rt.Fatal(err)
		}
		access, refresh := verified(rt, tk, renewed.Access), verified(rt, tk, renewed.Refresh)
		// Both lifetimes count from the refresh, not from the login.
		if refresh.ExpiresAt != now.Add(tk.RefreshTTL()).Unix() || access.ExpiresAt != now.Add(tk.AccessTTL()).Unix() {
			rt.Fatalf("lifetimes did not slide: access %+v, refresh %+v", access, refresh)
		}
		// The handles are fresh per token, and iat is the refresh's now.
		if access.JTI == old.JTI || refresh.JTI == old.JTI || access.JTI == refresh.JTI || access.IssuedAt != now.Unix() {
			rt.Fatalf("handles not fresh: access %+v, refresh %+v", access, refresh)
		}
		// Everything but the handles and the times is the old token's.
		for _, c := range []Claims{access, refresh} {
			if c.Username != old.Username || c.SecretKey != old.SecretKey || c.SessionID != old.SessionID || c.Generation != old.Generation || c.AuthTime != old.AuthTime {
				rt.Fatalf("renewed %+v does not continue %+v", c, old)
			}
		}
		// The old refresh token is multi-use: nothing retired it.
		if _, err := tk.Verify(g.Refresh); err != nil {
			rt.Fatalf("the old refresh token no longer verifies: %v", err)
		}
	})
}

// A key rotation propagates through Renew: a grant issued under the old
// passphrase renews under the new one, and once the old passphrase is
// gone the renewed grant verifies and the old refresh token does not.
func TestRotationThroughRenew(t *testing.T) {
	before := tokensFor(t, []string{"old"})
	g, err := before.Issue("alice", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	during := tokensFor(t, []string{"new", "old"})
	renewed, err := during.Renew(verified(t, during, g.Refresh))
	if err != nil {
		t.Fatal(err)
	}
	after := tokensFor(t, []string{"new"})
	verified(t, after, renewed.Access)
	verified(t, after, renewed.Refresh)
	if _, err := after.Verify(g.Refresh); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old refresh token after the rotation: %v", err)
	}
}

// Changing any argon2id cost re-derives key and kid, so a cost bump
// behaves as a key rotation.
func TestCostChangeRollsKeys(t *testing.T) {
	// This test is white-box on derive. The kid fingerprints passphrase,
	// salt and costs together, so a cost bump must roll it.
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
