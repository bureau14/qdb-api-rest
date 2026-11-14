# Plan: the release updater rewrites qdb-api-rest's VERSION file

Thread: rest VERSION registration in qdb-release (story 20143)
Status: approved

## Outcome

When the unit lands, qdb-release's version updater rewrites the root
`VERSION` file of qdb-api-rest, and it no longer targets the go-swagger
files `swagger.json`, `restapi/embedded_spec.go`,
`restapi/configure_qdb_api_rest.go` and `restapi/doc.go`. The updater
writes the qdb-nats-connector format: `3.15.0` for a release,
`3.15.0.dev3` for a nightly and `3.15.0.rc3` for a release candidate.
Pytest cases in qdb-release cover the three stages. In this repository,
the brief's versioning bullet names that format, and the log's Current
state lists neither story 20141 nor story 20143.

The work lands through the quasardb workflow on story 20143, with the
branch `sc-20143/rest-version-registration` in qdb-release and in
qdb-api-rest. Each repository gets one PR to master, assigned to Igor,
and is squash-merged. Neither PR is cherry-picked onto a 3.14.x branch.

Left for later:

- The release step that pins qdb-api-go in consumer repositories runs
  `go mod vendor`, which overwrites the hand-written patch in
  `vendor/github.com/duckdb/duckdb-go-bindings/alloc.go`. The unit does
  nothing about it, because the patch is temporary and story 20145
  removes it.
- The `rr-*` skills and the root `AGENTS.md` (Workflow) still describe
  the rr workflow. Retiring them is a separate change.

## Verified facts

- qdb-release's `qdb-api-rest` file entry rewrites `swagger.json`,
  `restapi/embedded_spec.go`, `restapi/configure_qdb_api_rest.go` and
  `restapi/doc.go`
  [qdb-release `scripts/release/src/submodules/updater/update.py:211-224`,
  master `d24a5ad`, 2026-11-13]. None of these files exist in
  qdb-api-rest on master [tree, 2026-11-13].
- qdb-release's `qdb-api-rest` format entry gives `x.y.z` for a release
  and `x.y.z-<stage>.<n>` for nightly and rc
  [`version.py:84-88`].
- qdb-release's `qdb-nats-connector` format entry gives `x.y.z`,
  `x.y.z.dev<n>` and `x.y.z.<stage><n>`, where the rc stage is the
  literal `rc` [`version.py:129-133`, `version.py:8-21`].
- qdb-release's `qdb-nats-connector` file entry rewrites `VERSION` with
  the regex
  `\A[0-9]+(?:\.[0-9]+){2}(?:\.(?:dev|rc)[0-9]+)?(?=\r?\n?\Z)`
  [`update.py:350-360`]. Two pytest cases cover it, one for a release and
  one for a nightly [`scripts/release/tests/test_version_mapper.py:139-165`].
- `update_files` expands each file name as a glob. A file that does not
  exist matches nothing, and the updater raises no error
  [`update.py:36-50`].
- `parts()` accepts only `x.y.z.p-(release|nightly|rc).n`
  [`version.py:8-21`].
- qdb-api-rest's `VERSION` holds `3.15.0.dev0` on master and
  `3.14.4.dev0` on 3.14.x [`VERSION`, `origin/3.14.x:VERSION`,
  2026-11-13]. The build reads it into the binary's ldflags and into the
  archive name [`scripts/cicd/20.build.sh:44-55`,
  `scripts/cicd/50.package.sh:87`].
- The deb and rpm jobs find the archive by the glob `*-rest.tar.gz`, and
  their package version comes from their own `common/get_version.sh`
  [qdb-pkg-debian and qdb-pkg-rpm `.buildkite/steps/_package.yml:11`,
  qdb-release `update.py`, the `qdb-pkg-debian` and `qdb-pkg-rpm`
  entries]. The format change does not reach them.
- qdb-release has no Buildkite pipeline in its tree. Its tests run with
  pytest from `scripts/release` (`tox.ini`, `requirements.txt`)
  [tree, 2026-11-13].
- qdb-release's 3.14.x branch has had no commits since 2025-09-15
  (`5a79201`). 3.14.2 was released from master (`b1d9d21`), and the
  3.14.4 preparation is on master (`f2f0777`, `d24a5ad`) [git log,
  2026-11-13].

## Design

qdb-release, `scripts/release/src/submodules/updater/version.py`, the
`qdb-api-rest` entry of the format table in `get`, in its current place:

```python
"qdb-api-rest": {
    # style: doc comment alone (no comment)
    # because: the qdb-nats-connector entry beside it carries none, and the
    #   three formats are clear from the code
    "release": xyz_ver,
    "nightly": "{}.dev{}".format(xyz_ver, stage_version),
    "rc": "{}.{}{}".format(xyz_ver, stage, stage_version),
},
```

qdb-release, `scripts/release/src/submodules/updater/update.py`, the
`qdb-api-rest` entry of the file table in `update_version_get`, in its
current place. It replaces the three go-swagger entries with one entry:

```python
"qdb-api-rest": [
    # style: doc comment alone (no comment)
    # because: it is the same entry as qdb-nats-connector's, which carries
    #   no comment
    {
        "files": ["VERSION"],
        "changes": [
            {
                # the same regex as qdb-nats-connector [update.py:355, Rationale row 3]
                "regex": r'\A[0-9]+(?:\.[0-9]+){2}(?:\.(?:dev|rc)[0-9]+)?(?=\r?\n?\Z)',
                "replace_with": ver,
            },
        ],
    },
],
```

qdb-release, `scripts/release/tests/test_version_mapper.py`, after the
qdb-nats-connector tests, in their shape:

```python
def test_qdb_api_rest__updates_version_file_for_release():
    # style: doc comment alone (no comment)
    # because: the qdb-nats-connector tests it mirrors carry none
    #
    # asserts the whole modification list for "3.15.0.0-release.0":
    #   files ["VERSION"], the regex, replace_with "3.15.0" [update.py entry]
    # asserts modify_content turns "3.15.0.dev0\n" into "3.15.0\n" [VERSION]


def test_qdb_api_rest__updates_version_file_for_nightly():
    # style: doc comment alone (no comment)
    # because: as above
    #
    # "3.15.0.0-nightly.3" gives replace_with "3.15.0.dev3" [version.py entry]
    # modify_content turns "3.15.0\n" into "3.15.0.dev3\n"


def test_qdb_api_rest__updates_version_file_for_rc():
    # style: doc comment alone (no comment)
    # because: as above
    #
    # "3.15.0.0-rc.2" gives replace_with "3.15.0.rc2" [version.py entry]
    # modify_content turns "3.15.0.dev3\n" into "3.15.0.rc2\n"
```

qdb-api-rest, `docs/brief.md`, Versioning and release, the bullet on the
version string location, rewritten whole:

```
- There is one version string location in this repository, a `VERSION`
  file at the repository root. `qdb-release`'s updater rewrites it at
  release time, with the version string format of qdb-nats-connector:
  `3.15.0` for a release, `3.15.0.dev3` for a nightly and `3.15.0.rc3`
  for a release candidate. [owner, 2026-11-13]
- (the rest of the bullet, on ldflags and on no version constants,
  stays as it is)
```

qdb-api-rest, `docs/log.md`, Current state:

```
- Last updated: the date of the commit.
- In flight: none. [20141 merged as #62, 1a1a2d48]
- Next: none. [20143 lands with this unit, Open question 1]
- A dated entry: rest-version-registration-plan.md deleted, facts moved
  to docs/brief.md.
```

## Rationale

| decision                                                                                    | why                                                                                                               | rejected, and why                                                                                                           | gained                                            | given up                                                 | settled by                              |
| ------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- | -------------------------------------------------------- | --------------------------------------- |
| 1. The VERSION file uses the qdb-nats-connector format (`x.y.z`, `x.y.z.devN`, `x.y.z.rcN`) | the VERSION files on both release lines already hold it, and the log names it                                     | the brief's `{xyz}-{stage}.{stage_version}`: it would rewrite both VERSION files and needs a regex that accepts two formats | one format and one regex for the two Go consumers | none known                                               | owner, 2026-11-13                       |
| 2. The updater targets only `VERSION`                                                       | the brief allows one version location, and the go-swagger files are gone                                          | keeping the old entries next to the new one: they match nothing without any error                                           | an entry that matches the tree                    | none                                                     | `docs/brief.md`, Versioning and release |
| 3. The qdb-api-rest entry repeats qdb-nats-connector's regex literal                        | every entry in `update.py` holds its regex inline                                                                 | a shared module constant: a pattern the file does not use, for two entries                                                  | the entry reads like its neighbours               | the two literals must change together                    | proposal                                |
| 4. The tests cover release, nightly and rc                                                  | the rc format is the one entry that differs from qdb-nats-connector's tests, which cover release and nightly only | two cases like qdb-nats-connector: the rc string would go untested                                                          | all three stages are pinned                       | one more test                                            | proposal                                |
| 5. The quasardb workflow in both repositories, both PRs assigned to Igor                    | the last three units merged that way, and the rr workflow does not cover qdb-release                              | the rr workflow's fast-forward merge                                                                                        | qdb-release is covered                            | none                                                     | owner, 2026-11-13                       |
| 6. qdb-release master only, no cherry-pick onto 3.14.x                                      | the 3.14 releases run from master                                                                                 | a cherry-pick onto the 3.14.x branch, which has had no commits since 2025-09-15                                             | one PR                                            | none                                                     | owner, 2026-11-13, confirmed with Igor  |
| 7. The release step that overwrites the vendored binding patch is not recorded or guarded   | the patch is temporary, and story 20145 removes it                                                                | a note in the log, or a guard in qdb-release's regeneration                                                                 | no work on a temporary problem                    | a release before 20145 needs the patch reapplied by hand | owner, 2026-11-13                       |

## Knowledge

- Commit 2 (version.py): no comment. Row 1 lands in commit 5.
- Commit 3 (update.py): no comment. Rows 2 and 3 are `plan only`,
  because the entry looks like its neighbour and no reader would ask why.
- Commit 4 (tests): no comment. Row 4 is `plan only`, because the test
  names show the three stages.
- Commit 5 (`docs/brief.md`): row 1 lands in the versioning bullet, as
  the owner's decision with its date.
- Rows 5, 6 and 7 are `plan only`. They are answers the owner gave in
  the conversation, and the code does not raise them again.
- Commit 7 (`docs/log.md`): the dated entry names `docs/brief.md` as the
  home of the moved facts.

## How the knowledge lands

The build stage is `/rr-build`. It writes every comment and document row
from sections 3 and 5 in the same commit as the code, and it reconciles
the code against this plan before the build-stage message. For this
unit, the qdb-release commits go on the branch
`sc-20143/rest-version-registration` in `/Users/lmergen/git/qdb-release`,
and the build runs pytest there before pushing. The ASCII and prettier
checks of `docs/AGENTS.md` apply to the two Markdown files in
qdb-api-rest only.

## Commits

1. `docs(plan): rest-version-registration-plan.md, the updater rewrites VERSION in the qdb-nats-connector format`
   (qdb-api-rest, this document)
2. `fix(updater): qdb-api-rest version uses the dev and rc suffixes of qdb-nats-connector`
   (qdb-release)
3. `fix(updater): qdb-api-rest entry rewrites VERSION instead of the go-swagger files`
   (qdb-release)
4. `test(updater): qdb-api-rest VERSION for release, nightly and rc`
   (qdb-release)
5. `docs(brief): the VERSION file uses the qdb-nats-connector version format`
   (qdb-api-rest)
6. `/doc-discipline check docs/rest-version-registration-plan.md`, on
   both branches. It produces a commit only if it finds something.
7. `docs(log): rest-version-registration-plan.md deleted, facts moved to docs/brief.md`
   (qdb-api-rest). The same commit rewrites Current state: In flight and
   Next become none, and Last updated gets the commit's date.
8. Verify:
   - qdb-release: run pytest from `scripts/release` in a project venv,
     then push `sc-20143/rest-version-registration` and open a PR to
     master, assigned to Igor, under story 20143.
   - qdb-api-rest: push `sc-20143/rest-version-registration` and open a
     PR to master, assigned to Igor. Build its head in Buildkite and
     wait for the result.
   - Green: report the build number and both PRs. Red: fix with
     further small commits on the branch, push, and build again.

## Open questions and recommendations

1. What does the log list under Next once 20143 lands? Recommendation:
   none. The open stories of epic 19567 are in the brief's tracking
   table, and the owner names the next unit.
