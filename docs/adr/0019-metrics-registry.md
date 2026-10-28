# ADR-0019: Metrics: the Prometheus client, the registry in the context, one namespace

Status: accepted
Date: 2026-10-28

## Context

The brief's Goal 9 asks for a Prometheus exposition with request and
query latencies, rows and bytes per format, time to first byte, pool
and breaker state, qdb call durations, the Go runtime and build info,
and M7 (`docs/log.md`) builds it. The tree carried no metrics library,
no `/metrics` route and no registry. Three choices constrain every
later series: which library produces the exposition, how the registry
reaches the code that records, and what every series is named. Each is
expensive to reverse once instruments are registered across packages.
The owner settled the three on 2026-10-28.

## Decision

1. `github.com/prometheus/client_golang` produces the exposition. One
   registry per process, a fresh `prometheus.NewRegistry`, built by
   `main` through `observe.NewMetrics`. The client's global default
   registry and its `promauto` convenience functions are not used.
2. The registry travels in the context through `observe.WithMetrics`
   and `observe.MetricsFrom`, which panics on a context without one, as
   `observe.Logger` does (ADR-0002). `main` places it once, right after
   the logger, and every listener and every package records into it.
3. Every series this binary names carries the `metrics.namespace` key
   as its prefix, `qdb_rest_` by default. The key is never empty, so
   the prefix never disappears, and it fits the Prometheus name
   grammar, which `internal/config` checks at startup. The client's
   own `go_` and `process_` series keep their names.
4. The build-info series is `<namespace>_build_info` at 1, with the labels
   `version`, `commit` and `build_mode` from `internal/build`, the three
   attributes of `main`'s starting line, so a dashboard reads what the
   log says.

## Consequences

- Pull only. A push to CloudWatch or another backend goes through a
  scraping agent or a collector, and a later OTLP reader sits beside
  the registry.
- Every test fixture that builds a context for a route or a package
  that records places `observe.NewMetrics()` on it, because
  `MetricsFrom` panics without one.
- A series name is a wire shape: the prefix, the name and the label set
  are read by dashboards, and a change to one is a change to the
  contract.

## Alternatives rejected

| Alternative                                        | Why not                                                                                                         |
| -------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| A hand-written exposition                          | histograms, the runtime series and the text format would be written and kept here                               |
| The OpenTelemetry SDK with its Prometheus exporter | it vendors `client_golang` underneath plus two modules and a views-and-readers layer the brief does not ask for |
| A no-op `MetricsFrom` on a context without metrics | a forgotten placement in `main` would serve an empty exposition without failing                                 |
| The registry as a constructor argument             | it breaks the carriage rule of `internal/AGENTS.md` and threads a value through every `Call`                    |
| The `qdb_` prefix                                  | it collides with qdbd or another component on the same Prometheus                                               |
| No prefix, or an empty namespace                   | `http_requests_total` collides with every Go service a host scrapes                                             |
| The prefix as a constant                           | two instances of this binary on one Prometheus, or a site's naming rule, need a prefix of their own             |
| The client's build-info collector                  | it reports the Go module version and not the ldflags-injected one                                               |
| `build_time` and `goamd64` as build-info labels    | the starting line does not carry them, and a label set is a wire shape                                          |
| The client's global default registry               | the brief forbids package-level mutable state, and two servers in one test binary would register twice          |
