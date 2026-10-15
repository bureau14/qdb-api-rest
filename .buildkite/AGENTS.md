# .buildkite/ -- Conventions

Scope: the Buildkite pipeline generator and step templates. The step
scripts the pipeline invokes live in `scripts/cicd/` (see its
`AGENTS.md`). qdb-nats-connector's pipeline is the reference for how
all of this should feel.

## Layout

- `pipeline.py` is the dynamic generator: 1 lint step, 8 per-platform
  build+unit-test steps and 1 aggregate test report. Run
  `python3 pipeline.py check` (needs `pip install -r requirements.txt`
  and `BUILDKITE_BRANCH` set) after any change, and `generate` prints
  the YAML for inspection.
- `steps/*.yml` are the step templates with `{placeholder}` vars, which
  `pipeline.py` loads and overlays.
- `tools/` is the `bureau14/qdb-cicd-tools` git submodule (the
  `qdb_pipeline` library). Never edit here. Improvements go upstream in
  that repo and land as a SHA bump, the same rule as
  `scripts/tests/setup`.

## Facts

- The web pipeline's entrypoint is `python3 pipeline.py [generate|check]`
  with this directory's `requirements.txt`, the same convention as
  `master`. Keep it, so no branch needs a web-side pipeline change.
- The platform matrix mirrors quasardb's pipeline name for name. The
  qdb-artifacts download variant derives from the platform slug, which
  is what wires the C-API dependency up. The c-api, server and utils
  dists are extracted into `qdb/`, the QuasarDB project layout that the
  CGO environment (`scripts/cicd/AGENTS.md`) and `scripts/tests/setup/`
  expect.
- Every download project is asked for this repository's own git ref.
  The plugin walks that ref, then the project's master, then main, and
  takes the first one that holds artifacts, so a branch of this
  repository gets the dependency's master without `pipeline.py` naming
  it. A `by_project` ref is only for pinning a dependency to one of its
  branches for a while, and `pipeline.py` says when such a pin goes.
- The second download project is `qdb-duck`, whose package (DuckDB with
  the quasar extension linked in) is extracted into `duck/`. qdb-duck
  uploads under the same platform slugs this pipeline uses, so the
  variant is the step's own. The lint step downloads it too, because
  golangci-lint's typecheck compiles `internal/olap`. The link flags and
  build tags that consume `duck/` are the root `.envrc`'s.
- qdbd runs in CI. The build step starts it via
  `scripts/tests/setup/start-services.sh` before building, and
  `hooks/pre-exit` stops it. The e2e harness in `tests/e2e/` runs last
  in the build step, after the Go tests, through
  `scripts/cicd/40.test-e2e.sh` (ADR-0015, and `docs/e2e.md`, "In
  Buildkite", specifies the step). The hook does not stop the REST
  servers the harness starts, because the harness Makefile's exit trap
  does.
- Doubled `$$` in env values escapes Buildkite's upload-time
  interpolation so agent-side variables (`QDB_CICD_AGENT_*`) survive to
  the agent shell.
- The org-wide `branch_configuration` (`master 3.14.x`) gates only
  webhook-triggered builds. Build a feature branch through the API with
  `ignore_pipeline_branch_filters: true`. The `bk` CLI does not set it,
  so its 422 on a feature branch means "filtered" rather than "blocked".
- Build creation takes the full 40-character commit SHA
  (`git rev-parse HEAD`). An abbreviated or hand-expanded SHA fails at
  checkout as GitHub's `upload-pack: not our ref`, which is not a
  replication problem.
