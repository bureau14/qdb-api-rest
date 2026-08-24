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

- The Buildkite web pipeline (operator-managed) is expected to have a
  single configured step that installs `requirements.txt` and pipes
  `pipeline.py generate` into `buildkite-agent pipeline upload`;
  everything else lives in this directory.
- The platform matrix mirrors quasardb's pipeline name for name; the
  qdb-artifacts download variant derives from the platform slug, which
  is what wires the C-API dependency up. Only the c-api archive is
  downloaded: nothing links it yet (M1 vendors qdb-api-go), but
  `cicd_assert_qdb_tree` proves the artifact dance on every platform.
- The e2e harness is deliberately NOT in CI (owner decision, docs/log.md
  2026-08-24). When it returns: add the server/utils archives to the
  download blocks, start qdbd via `scripts/tests/setup/`, add a
  `hooks/pre-exit` that stops services, and budget the step timeouts up.
- Doubled `$$` in env values escapes Buildkite's upload-time
  interpolation so agent-side variables (`QDB_CICD_AGENT_*`) survive to
  the agent shell.
