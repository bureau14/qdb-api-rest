# Architecture Decision Records

Numbered, append-only. Template: `0000-template.md`. When to write one and
what stays in plan decision-log tables: `docs/AGENTS.md`, "ADRs".

| ADR                                        | Title                                                                  | Status   |
| ------------------------------------------ | ---------------------------------------------------------------------- | -------- |
| [0001](0001-tls-certificates.md)           | TLS certificates: PEM files, ephemeral self-signed default             | accepted |
| [0002](0002-context-carried-logging.md)    | Logging: the logger travels in context                                 | accepted |
| [0004](0004-pure-probes.md)                | Probes and observability endpoints stay pure                           | accepted |
| [0005](0005-token-cryptography.md)         | Token cryptography: hand-rolled compact JWE, dir + A256GCM             | accepted |
| [0006](0006-clock-injection.md)            | The clock is an injected function, not context or synctest             | accepted |
| [0007](0007-legacy-compatibility-layer.md) | Legacy compatibility layer: v2 first, v1 wraps v2, one package         | accepted |
| [0008](0008-legacy-path-spelling.md)       | Legacy paths: /api/v1 canonical, unversioned aliases, never a redirect | accepted |
