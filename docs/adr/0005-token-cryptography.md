# ADR-0005: Token cryptography: hand-rolled compact JWE, dir + A256GCM

Status: proposed
Date: 2026-08-31
Milestone: M1

## Context

The brief (Authentication) locks the token design: JWE with modern
primitives, claims carrying reconnect material, keys derived from
passphrases via argon2id + HKDF, rolling keys through the
`auth.token_secrets` list, an ephemeral key when nothing is configured,
and 12h tokens minted by legacy `/api/login`. M1's exit requires this
ADR -- the JWE library, AEAD, and key derivation choices -- accepted
before `internal/auth` lands.

The decisive structural fact: the token has exactly one producer and one
consumer, both this binary. Clients treat it as opaque (brief,
Compatibility contract), and old-server tokens do not survive the
upgrade, so no foreign or legacy-format token is ever parsed. JOSE
libraries exist for algorithm agility and interop with tokens minted
elsewhere; here that generality sits on the request path as pure attack
surface -- the historical JWE/JWS CVE record (algorithm confusion,
oversized headers, compression bombs) lives almost entirely in features
a single-header verifier never uses.

With `alg: dir` there is no key-wrapping step, so compact JWE
degenerates to header + empty key segment + IV + ciphertext + tag --
roughly 200 lines including strict validation. On Go 1.27 the
cryptography itself is stdlib (`crypto/cipher` AES-GCM, `crypto/hkdf`,
`crypto/rand`); only argon2id needs `golang.org/x/crypto`.

## Decision

1. **Format**: RFC 7516 compact JWE. The protected header is exactly
   `{"alg":"dir","enc":"A256GCM","kid":"<derived>"}`: direct symmetric
   encryption (the derived key is the CEK, the encrypted-key segment is
   empty), AES-256-GCM content encryption, a random 96-bit IV per
   token, the encoded protected header as AAD. The verifier is an
   allowlist: a token is rejected unless its header carries exactly an
   accepted `(alg, enc)` pair and a `kid` present in the keychain -- no
   `zip`, no `crit`, no unknown parameters, no other algorithms.
2. **Implementation**: written in `internal/auth` over the stdlib
   primitives above. `golang.org/x/crypto` (argon2id) is the only new
   runtime dependency; no JOSE library links into the binary.
3. **Key derivation**, once at startup, per passphrase in
   `auth.token_secrets`: argon2id (32-byte output) with the fixed
   application salt `"qdb-rest/token-secrets/v1"` -- there is no
   per-user storage to hold a random one, the derivation's purpose is
   cost per attacker guess (~100ms), not per-record uniqueness, and the
   constant domain-separates our derivations from any other argon2id
   user while its `v1` suffix versions the scheme (changing it rolls
   every key and `kid`, i.e. a key rotation) -- then HKDF-SHA-256 with
   distinct info strings derives the 32-byte encryption key and the
   `kid` independently. The argon2id cost parameters (time, memory,
   parallelism) are configuration, so operators tune the toll to their
   hardware; defaults follow RFC 9106 and live in `internal/config`,
   not here. The parameters join the passphrase as derivation input:
   every instance behind a load balancer must run the same values,
   exactly as it must share `token_secrets`, and changing them changes
   every derived key and `kid`, so a tuning change behaves as a key
   rotation. They are never read from a token: the verifier derives
   nothing per token (a `kid` lookup into the keychain), and honoring
   header-supplied KDF parameters is a known DoS class (PBES2 `p2c`).
   The ephemeral startup key (no secret configured) skips argon2id -- a
   random key needs no stretching -- and flows through the same HKDF
   split.
4. **Conformance**: `go-jose/v4` is a test-only dependency. Property
   tests assert both directions -- every token we mint decrypts under
   go-jose, and tokens go-jose encrypts under our derived keys verify
   with us. The cross-check proves the encoding is conformant and
   interoperable (a self-consistent-but-wrong encoder passes its own
   roundtrip and fails only against an independent implementation); the
   security argument itself rests on the AEAD, the header allowlist,
   and key handling.
5. **One verifier** serves all three `typ` profiles (access, refresh,
   legacy 12h). TTLs are configuration defaults, not part of this
   decision.

## Consequences

- This repo owns a security-critical encoder/decoder. It is small
  enough to audit exhaustively, and review must treat it as crypto
  code: strict parse-then-decrypt order, nothing released from a token
  that failed authentication.
- The GCM bound is a non-issue at this volume: with `dir`, one key
  should stay under ~2^32 encryptions (random-IV collision bound);
  tokens are minted per login/refresh, not per request. If minting
  volume ever changes class, `A256KW` is the recorded escape hatch.
- `vendor/` gains `x/crypto` (runtime) and `go-jose/v4` (tests only;
  `go mod vendor` includes test dependencies, so it sits in the tree
  and in dependency-audit scope without linking into `bin/qdb_rest`).
- Future protocol changes are rotation-shaped: new header values enter
  the verifier allowlist while old ones age out within one refresh
  cycle or the 12h legacy window; clients never coordinate, because
  tokens are opaque.
- Interop stays open: `dir` + `A256GCM` compact JWE is readable by any
  JOSE library should another service ever need to verify tokens.

## Alternatives rejected

| Alternative                               | Why not                                                                                                                                                                                                      |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| go-jose/v4 at runtime                     | A general JOSE parser on the request path for a format with one accepted header; its API machinery is surplus when strict matching is ~50 lines. Retained as the test-side cross-check.                      |
| lestrrat-go/jwx                           | The most featureful option and the heaviest vendor footprint; JWKS, remote key sets, and non-compact serializations have no consumer here. The right tool for verifying foreign tokens, which never happens. |
| Committed golden test vectors             | Static vectors cover only themselves and rot as claims evolve; a live independent implementation exercises every property-test case. Owner decision 2026-08-31.                                              |
| `alg: A256KW`                             | A fresh CEK per token sidesteps the GCM per-key IV bound, at +40 bytes and an extra AES operation; the bound is unreachable at token-minting volume. Escape hatch if volume changes class.                   |
| `enc` ChaCha20-Poly1305 (XC20P)           | Draft-only, never standardized in RFC 7518; tokens would silently stop being JWE. AES-GCM is stdlib and hardware-accelerated on every CI platform.                                                           |
| `enc: A256CBC-HS512`                      | The MAC-then-encrypt composite family the old server used; two primitives and larger tokens where GCM is one AEAD.                                                                                           |
| RSA-OAEP-256 + A128CBC-HS256 (old server) | Asymmetric wrapping buys nothing when one party is both sender and receiver; the brief names it the superseded baseline, and old tokens do not survive the upgrade.                                          |
