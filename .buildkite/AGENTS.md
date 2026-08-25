# .buildkite/ -- Conventions

Scope: the Buildkite pipeline generator and step templates. The step
scripts the pipeline invokes live in `scripts/cicd/` (see its
`AGENTS.md`); qdb-nats-connector's pipeline is the reference for how
all of this should feel.

## Layout

- `pipeline.py` -- dynamic generator: 1 lint step + 8 per-platform
  build+unit-test steps + 1 aggregate test report. Run
  `python3 pipeline.py check` (needs `pip install -r requirements.txt`
  and `BUILDKITE_BRANCH` set) after any change; `generate` prints the
  YAML for inspection.
- `steps/*.yml` -- step templates with `{placeholder}` vars, loaded and
  overlaid by `pipeline.py`.
- `tools/` -- the `bureau14/qdb-cicd-tools` git submodule (the
  `qdb_pipeline` library). Never edit here; improvements go upstream in
  that repo and land as a SHA bump. Same rule as `scripts/tests/setup`.

## Facts

- The Buildkite web pipeline for qdb-api-rest already exists and drives
  `master`'s `.buildkite/pipeline.py` (the legacy server's pipeline uses
  the same dynamic-generator convention). This directory keeps that
  entrypoint -- `python3 pipeline.py [generate|check]`, same
  `requirements.txt` -- so builds of this branch need no web-side
  change.
- The platform matrix mirrors quasardb's pipeline name for name; the
  qdb-artifacts download variant derives from the platform slug, which
  is what wires the C-API dependency up. Only the c-api archive is
  downloaded; the vendored qdb-api-go links it (static `libqdb_api.a`
  on Linux, the shared library elsewhere) and `cicd_assert_qdb_tree`
  fails fast when it is absent.
- The e2e harness is deliberately NOT in CI (owner decision 2026-08-24,
  until further notice). When it returns: add the server/utils archives
  to the download blocks, start qdbd via `scripts/tests/setup/`, add a
  `hooks/pre-exit` that stops services, and budget the step timeouts up.
- Doubled `$$` in env values escapes Buildkite's upload-time
  interpolation so agent-side variables (`QDB_CICD_AGENT_*`) survive to
  the agent shell.
- The org-wide `branch_configuration` (`master 3.14.x`) gates only
  webhook-triggered builds. Build a feature branch through the API with
  `ignore_pipeline_branch_filters: true`; the `bk` CLI does not set it,
  so its 422 on a feature branch means "filtered", not "blocked".
- Build creation takes the full 40-character commit SHA
  (`git rev-parse HEAD`). An abbreviated or hand-expanded SHA fails at
  checkout as GitHub's `upload-pack: not our ref`, which is not a
  replication problem.
