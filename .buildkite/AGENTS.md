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

- The web pipeline's entrypoint is `python3 pipeline.py [generate|check]`
  with this directory's `requirements.txt`, the same convention as
  `master`; keep it, so no branch ever needs a web-side pipeline change.
- The platform matrix mirrors quasardb's pipeline name for name; the
  qdb-artifacts download variant derives from the platform slug, which
  is what wires the C-API dependency up. The c-api, server and utils
  dists are extracted into `qdb/`, the QuasarDB project layout that the
  CGO environment (`scripts/cicd/AGENTS.md`) and `scripts/tests/setup/`
  expect.
- qdbd runs in CI: the build step starts it via
  `scripts/tests/setup/start-services.sh` before building, and
  `hooks/pre-exit` stops it. The e2e harness in `tests/e2e/` joins the
  build step after the Go tests (ADR-0013; the step is specified in
  `docs/e2e.md`, "In Buildkite"); until its step script exists it
  is not in CI. The services and dists it needs are already present.
  Both daemons' logs, console output and error dumps are uploaded with
  the test report through the plugin's `job.artifacts` block
  (`steps/_build.yml`), the way quasardb's test step uploads its server
  logs, together with the rapid fail files of a failed property test:
  the exact draws of the failing case, which rapid writes on the flaky
  verdict as well, gitignored so nothing else keeps them, and replayable
  with `-rapid.failfile`. On Windows the same block uploads
  `logs/windows-events-*.txt`, the Application and Defender event log
  entries and the `qdbd.exe` processes alive at the end of the test
  step, because the insecure daemon has died there with nothing in its
  own log (`scripts/cicd/00.common.sh`, `cicd_record_windows_events`),
  and `logs/qdbd-watch-*.txt`, every `qdbd.exe`'s exit code and its
  memory and thread samples (`scripts/cicd/windows-qdbd-watch.ps1`).
  The archive is made inside the test command (`scripts/cicd/AGENTS.md`),
  because post-command and pre-exit hooks run plugins first and the
  repository last, so nothing a hook produces reaches the upload.
  Buildkite's `artifact_paths` is not used: the logs are not a release
  artifact.
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
