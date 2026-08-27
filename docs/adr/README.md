# Architecture Decision Records

Numbered, append-only. Template: `0000-template.md`. When to write one and
what stays in plan decision-log tables: `docs/AGENTS.md`, "ADRs".

| ADR                                     | Title                                                      | Status   |
| --------------------------------------- | ---------------------------------------------------------- | -------- |
| [0001](0001-tls-certificates.md)        | TLS certificates: PEM files, ephemeral self-signed default | accepted |
| [0002](0002-context-carried-logging.md) | Logging: the logger travels in context                     | accepted |
| [0003](0003-handle-pool.md)             | QuasarDB handle pool: checkout model, budget, failsafe     | proposed |
