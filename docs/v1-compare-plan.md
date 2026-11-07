# Plan: the side-by-side comparison of v1 with the old server

Thread: v1 side-by-side comparison
Status: draft

## Outcome

When the unit lands, `make test-v1-compare` in `tests/e2e` builds the
old server from `master`, starts it next to the new one on the secure
cluster and then on the insecure one, sends each v1 case to both and
compares the answers: query bodies as bytes, error bodies by status and
shape, the login's token by shape. The run is by hand on a developer
machine and never in Buildkite. It stores nothing. A green run is the
evidence for M9 exit criteria 2 and 3.

The unit leaves for later: the gzip case waits for the unit that makes
the v1 routes honor `Accept-Encoding` (owner, 2026-11-07), so commit 10
lands only after that unit has merged. Closing M9 is the owner's event
and not a commit of this unit.

## Verified facts

- The old server builds from a `git archive master` export with the
  repository's `qdb/` symlinked into it and its own `.envrc` sourced,
  under the repository's toolchain, and it starts and answers a login
  against the local qdbd (session of 2026-11-06, in a `/tmp` export
  since deleted). The export's `go.mod` says `go 1.23.0` and builds
  under the 1.27 toolchain.
- The old server reads one JSON config file (`--config-file`), with
  `cluster_uri`, `cluster_public_key_file`, `rest_private_key_file`,
  `tls_certificate`, `tls_key`, `tls_port`, `host`, `port`, `log`,
  `max_in_buffer_size`, `parallelism_count`
  (`master:config/config.go:16-33`, `master:qdb_rest.local.conf.sample`).
- Security is on when both `cluster_public_key_file` and
  `rest_private_key_file` are set (`master:config/config.go:267-269`),
  and a cluster key without a TLS certificate and key is fatal
  (`master:config/config.go:275-278`). The HTTPS listener is added when
  both TLS files are set (`master:apps/qdb_rest/main.go:60-64`).
- On a secure cluster the old server signs its tokens with the TLS
  private key, which must be an RSA key in PKCS8 PEM
  (`master:restapi/configure_qdb_api_rest.go:245-249`,
  `master:qdbinterface/credentials.go:89-101`). Insecure uses a key
  baked into the binary.
- The old server authenticates from the `Authorization` header with an
  optional `Bearer ` prefix (`master:restapi/configure_qdb_api_rest.go:329-333`)
  and from a `token` query parameter (`master:swagger.json:24-28`).
- The old query answers each cell as the binding's value: a null cell
  is nil, a timestamp is `time.Unix` in the process's zone, a count is
  typed `count` (`master:vendor/github.com/bureau14/qdb-api-go/v3/query.go:118-133`,
  `master:qdbinterface/query.go:68-95`). Under `TZ=UTC`, which
  `common.sh` exports for every server it starts, the timestamp
  marshals as the new v1's UTC time (`internal/httpapi/v1/tables.go:79-81`).
- The old server gzips every response whose request's `Accept-Encoding`
  contains `gzip` (`master:restapi/configure_qdb_api_rest.go:834-849`);
  the new v1 routes answer identity (`internal/httpapi/v1/query.go:40`).
- The generated rows carry nulls in every nullable column
  (`internal/qdbtest/table/table.go:123-131`, `docs/e2e.md`, Lineage),
  so a `SELECT *` over them exercises nulls. The CSV decoder reads an
  empty field as null (`internal/encoding/csv.go:26-37`).
- The new v1 cases are listed in `internal/httpapi/v1/login_test.go:79-116`
  and `internal/httpapi/v1/query_test.go:198-424`.
- `remove_tag` lives in `round-trip.sh:210-216` and is the one helper
  the comparison needs that `common.sh` does not hold.
- `openssl` 3.6.5 and `jq` are on the owner's machine; `openssl req
-newkey` writes a PKCS8 key.

## Design

### `tests/e2e/Makefile`

```make
# The old server (docs/e2e.md, The v1 comparison): master exported and
# built against this repository's qdb/, by hand, for the comparison.
#   style: doc comment + inline notes
#   because: three paths and four ports a reader would otherwise look for
#   - OLD_SRC is tests/e2e/old-server, gitignored [Rationale 6]
#   - OLD_BIN is bin/qdb_rest_old$(EXE_EXT), next to qdb_rest [tests/e2e/AGENTS.md, the tool's bin/]
#   - the old server takes two ports per cluster, http and tls, because a secure old server must bind tls [master:config/config.go:275-278]
#   - OLD_LOG* are its log files, because the old server writes a file and not stdout [master:config/config.go:27]
OLD_SRC, OLD_BIN, OLD_PORT, OLD_TLS_PORT, OLD_PORT_SECURE, OLD_TLS_PORT_SECURE,
OLD_PID*, OLD_LOG*, OLD_TLS_CERT, OLD_TLS_KEY, COMPARE_OUT, COMPARE := ...

old-server:
	# style: doc comment + inline notes
	# because: an export and a symlink are two lines a reader would replace with a worktree
	# - git archive master, extracted into OLD_SRC, leaves .git untouched [Rationale 6]
	# - qdb/ is symlinked, because the export's .envrc resolves qdb/ next to itself [master:.envrc:31-33]
	# - the build sources the export's own .envrc and uses its vendor [verified 2026-11-06]

test-v1-compare: services-check e2etool old-server
	# style: doc comment + overview + inline notes
	# because: four servers start and stop in an order the trap must match
	# 1. the tls pair for the secure old server [Rationale 8]
	# 2. the secure old server and the secure new server, the driver, both stopped
	# 3. the insecure old server and the insecure new server, the driver, both stopped
	# 1. an RSA key, because the old server signs tokens with it [master:restapi/configure_qdb_api_rest.go:245]
	# 2. secure first, because the login only means something there [Rationale 1]
	# 3. the new servers reuse REST_ARGS and REST_ARGS_SECURE, so the comparison and the round trip test one binary the same way
```

### `tests/e2e/common.sh`

```bash
# old_config <file> <cluster_uri> <port> <tls_port> <log> [<cluster_key> <user_key> <cert> <key>]:
# writes the old server's JSON config. The four key paths make it a
# secure config; without them it is insecure.
old_config() {
    # style: doc comment + inline notes
    # because: two keys carry values a reader would not expect
    # - host is 127.0.0.1, because localhost may resolve to ::1 and the harness dials v4 [tests/e2e/Makefile, REST_ARGS]
    # - max_in_buffer_size and parallelism_count are the sample's values [master:qdb_rest.local.conf.sample]
    # - rest_private_key_file is the harness's user key, because security is on only when it is set [master:config/config.go:267-269]
}

# old_tls_pair <cert> <key>: a self-signed RSA pair for the secure old
# server, written once per run.
old_tls_pair() {
    # style: doc comment + inline notes
    # because: the key type is the one line a reader would change
    # - rsa, because the old server unmarshals its token key as PKCS8 RSA [master:qdbinterface/credentials.go:95-101]
}

# remove_tag <tag> <qdbsh cluster options...>: moved from round-trip.sh
# unchanged, with its comment block rewritten from the code.
```

### `tests/e2e/compare-v1.sh`

```bash
# The v1 comparison (docs/e2e.md, The v1 comparison): one cluster per
# call. Each case goes to the old server at the unversioned path and to
# the new server at /api/v1/<path> and at the alias; query bodies
# compare as bytes, errors by status and shape, the token by shape.
#
#   compare-v1.sh --name <cluster> --old <url> --new <url> --tool <e2etool> --out <dir> --rows N --seed S --cluster-opts "<qdbsh options>" [--login-body <file>]
#
#   style: file header, as stress.sh
#   - the Makefile is the only source of URLs and paths [tests/e2e/AGENTS.md]
#   - no case sends #sql, and no case exercises a listed deviation [Rationale 3, 4]

# shape_of <body_file>: the body with every scalar replaced by its JSON
# type and every array cut to its first element, on stdout.
shape_of() {
    # style: doc comment alone
    # because: one jq filter, and the doc comment says what it computes
}

# send_case <name> <url> <path> <curl args...>: one request to one
# server, body and headers under out/<name>.<server>, status on stdout.
# A wrapper over request that names the file by server.
send_case() { ... }   # style: doc comment alone

# compare_bytes <name> <old_status> <new_status>: statuses equal and the
# two bodies equal by cmp; on a mismatch the first differing bytes.
compare_bytes() {
    # style: doc comment + inline notes
    # - cmp -l head, because the bodies are one line and a diff shows nothing [proposal]
}

# compare_shape <name> <old_status> <new_status>: statuses equal and
# shape_of equal.
compare_shape() { ... }   # style: doc comment alone

# v1_case <kind> <name> <path> <curl args...>: sends the case to the old
# server at /api/<path> and to the new one at /api/v1/<path> and
# /api/<path>, and compares the old answer with each of the new ones by
# <kind>, bytes or shape.
v1_case() {
    # style: doc comment + inline notes
    # because: the three sends and two compares are the whole mechanism
    # - the old server knows the unversioned spelling only [ADR-0008, Consequences]
    # - the new server answers both, and each is compared with the old [ADR-0008, Consequences]
}

# login_case: the login on both servers, compared by shape and a
# non-empty token; prints OLD_TOKEN and NEW_TOKEN.
login_case() {
    # style: doc comment + inline notes
    # - the tokens differ by construction, so the shape and not the bytes [docs/brief.md, POST /api/v1/login]
    # - the two tokens are kept apart, because each server verifies only its own [docs/brief.md, tokens minted by the old server are rejected]
}

# query_case <name> <query>: the query on both servers under each
# server's token, compared as bytes.
query_case() { ... }   # style: doc comment alone

# error_case <name> <curl args...>: a request that fails on both,
# compared by status and shape.
error_case() { ... }   # style: doc comment alone

# query_rows: SELECT * and one SELECT per column over the generated
# table, and the all-null column and no rows over the null table.
query_rows() {
    # style: doc comment + inline notes
    # - a hand-written CSV of two rows with an empty v_int64, because the generator draws nulls per cell and never a whole column [internal/qdbtest/table/table.go:123-131]
    # - IN RANGE(1970, +1d), because no generated row is there [tests/e2e/tools/e2etool/main.go:53-55]
    # - no COUNT, because its type word is a listed deviation [docs/brief.md, Deliberate deviations]
}

# error_rows: no token, a bad token, an empty body, a bare string, an
# undecodable body, an invalid query and an unknown table.
error_rows() {
    # style: doc comment + inline notes
    # - the rows are the Go test's [internal/httpapi/v1/query_test.go:277-285]
    # - messages are not compared, because each server words its own [docs/log.md, M9 exit criterion 2]
}

# token_rows: the bearer header, ?token=, and a bad header with a good
# ?token=, each a SELECT compared as bytes.
token_rows() {
    # style: doc comment + inline notes
    # - the old server reads ?token= too [master:swagger.json:24-28]
}

# find_rows: a tag that matches, a tag that does not, the finder prefix
# and a leading space.
find_rows() {
    # style: doc comment + inline notes
    # - the tag is attached through the new server's v2 route, after remove_tag [tests/e2e/AGENTS.md, the tag entry outlives its carriers]
    # - finder and the leading space are errors on both, compared by shape [docs/brief.md, Warts preserved]
}

# gzip_rows: the login and a SELECT under Accept-Encoding: gzip,
# content-encoding compared, the bodies compared after gunzip.
gzip_rows() {
    # style: doc comment + inline notes
    # - lands after the compression unit [Rationale 2]
}

# run_cluster: login, the generated table ingested through the new
# server, the null table, then the rows in order.
run_cluster() {
    # style: doc comment + overview + inline notes
    # because: the ingest must precede every query and the tag every find
    # 1. login_case
    # 2. gen, create and ingest e2e_v1 as CSV through the new server
    # 3. create and ingest e2e_v1_null from the hand-written CSV
    # 4. query_rows, error_rows, token_rows, find_rows, gzip_rows
    # 2. through the new server's v2, because the old server has no ingest and the rows are generated [ADR-0015, Decision 3]
    # 3. under the new server's token, because the v2 routes are the new server's alone
}

main() { parse_args; mkdir out; anonymous body when none; run_cluster || die; log passed }
```

### `tests/e2e/.gitignore` (root `.gitignore`)

```
# the old server's export, built by make old-server
tests/e2e/old-server/
```

### `docs/e2e.md`, after Stress definition

```markdown
## The v1 comparison

- what it is: the old server from `master` and the new one on the same
  cluster, each v1 case to both, bytes, shape, token (ADR-0015, D4)
- by hand, developer machines only, never Buildkite (owner, 2026-11-07)
- secure cluster first, then insecure (owner, 2026-11-07)
- the old server: export, qdb/ symlink, own .envrc; JSON config; the RSA
  TLS pair it signs tokens with
- the cases, grouped: login, query rows, error rows, token rows, find
  rows, gzip rows; which are bytes and which are shape
- what is left out and why: #sql (owner, 2026-11-07), every listed
  deviation (brief), TZ=UTC on both so the UTC deviation never shows
- Layout rows: compare-v1.sh, old-server/, the Makefile line
- Decision log (2026-11-07): export over worktree; compare helpers in
  the driver; secure first
```

### `tests/e2e/AGENTS.md`

```markdown
- The v1 comparison (`compare-v1.sh`, `make test-v1-compare`) runs by
  hand and never in CI. `make old-server` exports `master` into
  `old-server/` and builds it against the repository's `qdb/`; the
  export is never edited. The old server's config and TLS pair are
  written by `common.sh`. No case sends `#sql` and no case exercises
  a deviation the brief lists. remove_tag is in common.sh.
```

### `tests/e2e/README.md`

```markdown
## The v1 comparison

- one paragraph in the shape of The stress: what it starts, what it
  compares, by hand, `COMPARE_ROWS`, `COMPARE_SEED`, working files
```

## Rationale

| decision                                                                                            | why                                                                                                      | rejected, and why                                                                                | gained                        | given up                                        | settled by                                                                                  |
| --------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ | ----------------------------- | ----------------------------------------------- | ------------------------------------------------------------------------------------------- |
| 1. The secure cluster runs first, then the insecure one                                             | the login means something only on the secure cluster, and the anonymous login is the plugin's path       | secure only: the anonymous login would be compared with nothing                                  | both logins compared          | two more servers per run                        | owner, 2026-11-07                                                                           |
| 2. The gzip case lands after the unit that makes v1 honor `Accept-Encoding`                         | the owner wants v1 to gzip as the old server did, and v2 everywhere it makes sense                       | dropping the case; comparing decoded bodies only                                                 | the header compared           | commit 10 waits                                 | owner, 2026-11-07                                                                           |
| 3. No case sends `#sql`                                                                             | the owner excludes `#sql` from the v1 comparison                                                         | listing it as a deviation: the owner chose not to                                                | nothing to explain            | `#sql` on v1 is covered by the Go test only     | owner, 2026-11-07                                                                           |
| 4. No case exercises a listed deviation: wrong secret, `COUNT`, NaN, an old token, the process zone | the brief says no case of the comparison exercises an entry of the table                                 | comparing them and expecting a difference: a comparison that expects a difference proves nothing | every red is a bug            | the deviations rest on the Go tests             | `docs/brief.md`, Deliberate deviations                                                      |
| 5. The rows are generated by `e2etool gen` and ingested through the new server's v2 ingest          | every suite works on rows it generates, and the old server has no ingest                                 | literal `INSERT`s through v1: a second writer of the rows                                        | nulls and every type for free | the comparison depends on v2 ingest             | ADR-0015, Decision 3                                                                        |
| 6. The old server is built from a `git archive master` export, not a worktree                       | the export leaves `.git` untouched and its own `.envrc` works with a `qdb/` symlink; verified 2026-11-06 | `git worktree add`: registers in `.git`, needs pruning, and a stale one blocks a branch          | one `rm -rf` cleans it        | a second checkout of `master` on disk           | proposal                                                                                    |
| 7. A separate driver and target, not steps of the round trip                                        | the round trip runs in CI on every platform and the comparison does not                                  | cases in `round-trip.sh` behind a flag: the CI driver would carry code it never runs             | the round trip unchanged      | a third driver                                  | proposal                                                                                    |
| 8. The old server gets a self-signed RSA pair written by the target                                 | it signs tokens with its TLS key, PKCS8 RSA                                                              | reusing the new server's ephemeral certificate: it is never written to disk                      | no checked-in key             | `openssl` required                              | `master:restapi/configure_qdb_api_rest.go:245`, `master:qdbinterface/credentials.go:95-101` |
| 9. The old server is sent the unversioned path, the new server both spellings                       | the old server knows no other, and the v1 spelling must answer identically                               | the alias only: the canonical spelling would be untested against the old server                  | both spellings proven         | three sends per case                            | ADR-0008, Consequences                                                                      |
| 10. Error bodies compare by status and shape, through a jq type walk                                | each server words its own messages                                                                       | comparing messages: every message would differ                                                   | shape proven                  | a message regression is not seen                | `docs/log.md`, M9 exit criterion 2; the jq walk is a proposal                               |
| 11. Both servers run under `TZ=UTC`                                                                 | `common.sh` exports it for every server it starts                                                        | the old server under a local zone to show the deviation: the brief leaves deviations out         | timestamps compare as bytes   | the UTC deviation is never shown                | `tests/e2e/common.sh`; `docs/brief.md`, Deliberate deviations                               |
| 12. The old-server helpers go in `common.sh`, the compare helpers in the driver                     | the Makefile sources `common.sh` and starts the servers; nothing else compares                           | everything in the driver: the Makefile could not start the old server                            | the stress pattern kept       | `common.sh` carries two helpers one target uses | proposal                                                                                    |
| 13. `remove_tag` moves to `common.sh`                                                               | a helper two drivers need lives in `common.sh`                                                           | a copy in the driver: forbidden                                                                  | one helper                    | the round trip's file changes                   | `tests/e2e/AGENTS.md`                                                                       |
| 14. The run stores nothing: working files under `actual/compare/`, cleaned by `make clean`          | validation runs on the owner's machine, and every stored golden would be one more file to audit          | a captured expected body: ADR-0015 removed them                                                  | no goldens                    | a failure is reproduced by rerunning            | ADR-0015, Decision 4                                                                        |
| 15. Closing M9 is not a commit of this unit                                                         | a milestone state change is the owner's event in the log                                                 | a log line "criterion 2 green": a test outcome, which the log does not record                    | the log stays the owner's     | the plan-stage message asks                     | `docs/AGENTS.md`, Entries                                                                   |

## Knowledge

- Commit 2: the Makefile comments of `old-server` (Rationale 6, 8 ports). `.gitignore` line. The `docs/e2e.md` Layout row waits for commit 12.
- Commit 3: `compare-v1.sh` header (Rationale 3, 4, 7), `shape_of`, `send_case`, `compare_bytes`, `compare_shape`, `v1_case` (Rationale 9), `login_case` (brief, login). `parse_args`, `main` bare as in `stress.sh`.
- Commit 4: `old_config`, `old_tls_pair` in `common.sh` (Rationale 8, 12), the `test-v1-compare` target's overview (Rationale 1).
- Commit 5: `query_case`, `query_rows` select star and per column, `run_cluster` steps 2 and 4 (Rationale 5).
- Commit 6: `query_rows` null table and no rows, `run_cluster` step 3 (Rationale 4 for `COUNT`, inline).
- Commit 7: `error_case`, `error_rows` (Rationale 10).
- Commit 8: `token_rows` (verified fact, `?token=`).
- Commit 9: `remove_tag` moves (Rationale 13), `find_rows`.
- Commit 10: `gzip_rows` (Rationale 2). Lands after the compression unit; see How the knowledge lands.
- Commit 11: the insecure block of the target; the anonymous body in `main` as the stress writes it.
- Commit 12: `docs/e2e.md` The v1 comparison (Rationale 1, 3, 4, 11, 14; the decision log carries 6, 7, 12), Layout rows; `tests/e2e/AGENTS.md` bullet (6, 12, 13); `README.md` section.
- Plan only: the gzip answer's note that v2 should compress everywhere (it seeds the other unit); the `/tmp` build of 2026-11-06; Rationale 15.
- No ADR: no decision here shapes more than the harness.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from the Design and Knowledge sections in the same commit as the
code and reconciles the code against this plan before the build-stage
message. Specific to this unit:

- Commit 4 is the first run of the secure old server from the harness.
  If the old server refuses the generated key or the config, the
  executor reads the old server's log file (`OLD_LOG_SECURE`), fixes
  the config or the openssl line, and records the fact in the Makefile
  comment; it does not change the old server.
- Commit 10 needs the v1 compression unit on the base. The executor
  checks `git log sc-19567/rest-rewrite` for it before writing the
  commit. When it is absent, the executor stops before commit 10 and
  asks the owner through the question tool whether to merge without it;
  the commits after 10 do not depend on it.
- Commit 5 is the first byte comparison. A mismatch is a finding about
  v1 and not about the comparison; the executor reports it in the
  build-stage message with the first differing bytes and does not
  change the case to make it pass.
- The docs rows of commit 12 cite the owner's answers of 2026-11-07
  with their date.

## Commits

1. `docs(plan): v1-compare-plan.md, the side-by-side comparison of v1 with the old server` (this document)
2. `test(e2e): make old-server exports master and builds it against the repository's qdb`
3. `test(e2e): compare-v1.sh compares the login's token by shape at every spelling`
4. `test(e2e): make test-v1-compare writes the old server's tls pair and config and runs the secure cluster`
5. `test(e2e): the query rows, select star and one select per column, byte for byte`
6. `test(e2e): the query rows of an all-null column and of no rows`
7. `test(e2e): the error rows compared by status and shape`
8. `test(e2e): the token rows, the bearer header, ?token= and a bad header with a good ?token=`
9. `test(e2e): the find rows, remove_tag moved to common.sh`
10. `test(e2e): the gzip rows compare the content-encoding header and the decoded bodies`
11. `test(e2e): the insecure cluster with the anonymous login`
12. `docs(e2e): the v1 comparison in docs/e2e.md, tests/e2e/AGENTS.md and the README`
13. `/doc-discipline all tests/e2e docs/e2e.md docs/v1-compare-plan.md`, then `/doc-discipline check` against the plan; findings fixed as further commits
14. `docs(log): v1-compare-plan.md deleted, facts moved to docs/e2e.md, tests/e2e/AGENTS.md and the comments of tests/e2e` (Current state: Next item 1 removed, In flight none)
15. Verify: push `sc-19567/rr-v1-compare`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. Whether this unit's build may start before the v1 compression unit
   has merged. Recommendation: yes; commits 2 to 9 and 11 to 14 do not
   depend on it, and commit 10 waits as How the knowledge lands says.
2. Whether a green run closes M9 in the log. Recommendation: the owner
   closes M9 after the run, as a dated entry, outside this unit.
