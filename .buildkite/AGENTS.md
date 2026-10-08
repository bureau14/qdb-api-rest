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
- qdbd runs in CI. The build step starts it via
  `scripts/tests/setup/start-services.sh` before building, and
  `hooks/pre-exit` stops it. The e2e harness in `tests/e2e/` joins the
  build step after the Go tests (ADR-0013, and `docs/e2e.md`, "In
  Buildkite", specifies the step). Until its step script exists it is
  not in CI. The services and dists it needs are already present.
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
