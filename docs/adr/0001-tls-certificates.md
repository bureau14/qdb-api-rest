# ADR-0001: TLS certificates: PEM files, ephemeral self-signed default

Status: accepted
Date: 2026-08-21

## Context

The HTTPS listener (deployment port 40443) needs a certificate source.
Customers overwhelmingly run this product inside their own networks
(brief: Non-goals, "no public-internet hardening"), very often with
self-signed certificates, and the brief's security-hygiene goal forbids
any cryptographic material baked into the binary. The candidate
mechanisms were operator-provided certificate files, ACME/Let's Encrypt
automation, AWS Certificate Manager integration, and a zero-config
self-signed mode.

The design also has a precedent to match. The auth layer (brief:
Authentication) generates an ephemeral token key at startup when no
secret is configured and logs a warning, which gives easy defaults,
honest warnings and no baked-in keys.

## Decision

The initial scope has two certificate sources:

1. **Configured PEM pair**: `tls.certificate` + `tls.private_key`
   (paths to PEM files), validated as both-or-neither. This is the
   universal mechanism, because corporate CAs, k8s cert-manager secrets,
   and ACM Private CA exports all reduce to a PEM pair on disk.
2. **Ephemeral self-signed fallback**: when the HTTPS listener is
   enabled and no pair is configured, the server generates an in-memory
   ECDSA P-256 self-signed certificate at startup and logs a warning
   carrying the certificate's sha256 fingerprint. The SANs are
   localhost, loopback v4/v6 and the machine hostname, and the validity
   is 10 years so expiry cannot stop a long-running process. TLS
   therefore works with zero configuration. The documented cost is a
   new identity on every start and per-instance identities behind a
   load balancer, which is the trade the ephemeral token key already
   makes.

Both listeners are on by default (`:40080` HTTP, `:40443` HTTPS,
matching the deployed shape), and an empty address disables a listener
(`--listen-https=` / `listen.https: ""`). The minimum TLS version is
1.2, set explicitly. Certificates are loaded once at startup, and
rotation is restart-based.

## Consequences

- `qdb_rest` with no configuration serves working TLS out of the box.
  Dev setups and the very common self-signed deployments need zero
  ceremony.
- Unconfigured production deployments are visible, because every start
  logs a warning with the fingerprint, which also lets careful clients
  pin it.
- The load-balancer-in-front pattern serves ACM users: the LB
  terminates TLS and health-checks the status probes. There is nothing
  to build or document beyond that sentence.
- Hot certificate reload (cert-manager renewals without restart, for
  example via `GetCertificate` re-reading on file change) is a purely
  additive later step, and nothing in this decision blocks it.
- ACME support, if ever demanded, is likewise additive config
  (`tls.acme: ...`) and does not disturb the file path.

## Alternatives rejected

| Alternative                     | Why not                                                                                                                                                                           |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Let's Encrypt / ACME (autocert) | It requires a public DNS name and inbound reachability on 80/443, and the product runs inside customer networks where neither holds. It is additive later if demand materializes. |
| AWS Certificate Manager         | ACM never releases private keys, so in-binary integration is impossible by design. ACM certificates terminate on AWS load balancers, which already front this server.             |
| Persisted generated certificate | State on disk (path, permissions, packaging, multi-instance semantics) for marginal gain, and it contradicts the ephemeral-key precedent and the stateless posture.               |
| Certificate hot reload now      | Real value for cert-manager deployments, but additive. The foundation milestone keeps the listener minimal, and restart-based rotation is acceptable at the current scope.        |
