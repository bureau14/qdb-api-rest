# ADR-0008: Legacy paths: /api/v1 canonical, unversioned aliases, never a redirect

Status: accepted
Date: 2026-09-01

## Context

The old server served its API unversioned (`/api/login`, `/api/query`,
`/api/status/*`) and the brief freezes that surface as v1 while minting
everything new under `/api/v2/*`. Existing clients -- the Grafana plugin
and customer code -- send the unversioned paths and cannot be changed;
the goldens captured from the old server pin those paths with direct
`200` responses. Inside this repository the same endpoints need one
spelling, or documentation, code and tests drift between two.

## Decision

1. **Canonical spelling is `/api/v1/<path>`.** The URI itself says the
   legacy protocol is in play. Documentation, code and tests refer to a
   legacy endpoint only in this form.
2. **The unversioned path is an alias.** Every legacy endpoint is also
   served at its historical unversioned path, by the same handler. The
   alias exists for clients that cannot be changed; it is never the
   spelling a new reference uses.
3. **An alias is never a redirect.** The alias serves the handler
   directly. A `307`/`308` on `POST` breaks conservative HTTP clients,
   changes observable behaviour, and contradicts the goldens.
4. **Unversioned means v1, except the probes.** A path under `/api/`
   with no version segment is the legacy protocol; new endpoints exist
   under `/api/v2/*` only. The status probes (`/api/status/*`) are the
   one exception: they are an operational surface for load balancers
   and orchestrators, not part of the application protocol, so they
   are neither legacy nor aliased under `/api/v1/`.

## Consequences

- Goldens, the e2e harness and the bench client keep the unversioned
  spelling, because the old server they are captured from and replayed
  against knows no other. Proving the `/api/v1/<path>` spelling answers
  identically is a separate check: every golden replays at both
  spellings.
- The status probes keep their unversioned paths as the paths load
  balancers are configured with; their `/api/v2/status/*` mirrors are
  the current-protocol spelling, not aliases of a legacy one. The probe
  goldens replay at the unversioned path only; the both-spellings check
  covers the protocol endpoints.
- Route registration in the legacy package lists each handler twice;
  that duplication is the whole aliasing mechanism.

## Alternatives rejected

| Alternative                                       | Why not                                                                                    |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Unversioned as the canonical spelling             | nothing in the URI says the legacy protocol is in play; v2 would look like the odd one out |
| Redirect the unversioned path to `/api/v1/<path>` | breaks `POST` on conservative clients; changes the pinned `200` shape                      |
| Serve only the unversioned path, no v1 alias      | leaves the versioning stance a documentation fiction                                       |
| Serve only `/api/v1/<path>`                       | breaks every existing client                                                               |
