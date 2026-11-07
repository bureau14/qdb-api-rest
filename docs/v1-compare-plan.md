# Plan: the side-by-side comparison of v1 with the old server

Thread: v1 side-by-side comparison
Status: draft

## Outcome

When the unit lands, `make test-v1-compare` in `tests/e2e` builds the
old server from `master`, starts it next to the new one on the secure
cluster and then on the insecure one, and runs one flow against each
pair: a login on both servers, then a list of queries sent to both
under each server's token. The login's token compares by shape. A
query that answers 200 compares as bytes, and one that fails compares
by status and the shape of its body. The cluster is seeded beforehand
by `make test-round-trip`, which leaves its `e2e_csv` table and the
`e2e_tag` tag on both clusters. The run is by hand on a developer
machine and never in Buildkite. It stores nothing. A green run is the
evidence for M9 exit criteria 2 and 3.

The unit leaves for later: a gzip comparison, which waits for the unit
that makes the v1 routes honor `Accept-Encoding` (owner, 2026-11-07)
and is one more query line once that unit exists. Closing M9 is the
owner's event and not a commit of this unit.

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
- The old query answers each cell as the binding's value: a null cell
  is nil, a timestamp is `time.Unix` in the process's zone, a count is
  typed `count` (`master:vendor/github.com/bureau14/qdb-api-go/v3/query.go:118-133`,
  `master:qdbinterface/query.go:68-95`). Under `TZ=UTC`, which
  `common.sh` exports for every server it starts, the timestamp
  marshals as the new v1's UTC time (`internal/httpapi/v1/tables.go:79-81`).
- A query beginning with `find` goes to the tag find on both servers
  (`master:qdbinterface/query.go:111`, `internal/httpapi/v1/AGENTS.md`).
- The round trip creates `e2e_csv`, `e2e_ndjson`, `e2e_arrow` and
  `e2e_json` on each cluster, ingests generated rows with nulls in
  every nullable column, tags each with `e2e_tag`, and drops nothing
  at its end (`tests/e2e/round-trip.sh:495-520`,
  `internal/qdbtest/table/table.go:123-131`).
- `openssl` 3.6.5 and `jq` are on the owner's machine; `openssl req
-newkey` writes a PKCS8 key.

## Design

### `tests/e2e/Makefile`

```make
# The old server (docs/e2e.md, The v1 comparison): master exported and
# built against this repository's qdb/, by hand, for the comparison.
#   style: doc comment + inline notes
#   because: three paths and four ports a reader would otherwise look for
#   - OLD_SRC is tests/e2e/old-server, gitignored [Rationale 5]
#   - OLD_BIN is bin/qdb_rest_old$(EXE_EXT), next to qdb_rest [tests/e2e/AGENTS.md, the tool's bin/]
#   - the old server takes two ports per cluster, http and tls, because a secure old server must bind tls [master:config/config.go:275-278]
#   - OLD_LOG* are its log files, because the old server writes a file and not stdout [master:config/config.go:27]
#   - COMPARE_TABLE defaults to e2e_csv, the round trip's first table [Rationale 4]
OLD_SRC, OLD_BIN, OLD_PORT, OLD_TLS_PORT, OLD_PORT_SECURE, OLD_TLS_PORT_SECURE,
OLD_PID*, OLD_LOG*, OLD_TLS_CERT, OLD_TLS_KEY, COMPARE_TABLE, COMPARE_OUT, COMPARE := ...

old-server:
	# style: doc comment + inline notes
	# because: an export and a symlink are two lines a reader would replace with a worktree
	# - git archive master, extracted into OLD_SRC, leaves .git untouched [Rationale 5]
	# - qdb/ is symlinked, because the export's .envrc resolves qdb/ next to itself [master:.envrc:31-33]
	# - the build sources the export's own .envrc and uses its vendor [verified 2026-11-06]

test-v1-compare: services-check old-server
	# style: doc comment + overview + inline notes
	# because: four servers start and stop in an order the trap must match
	# 1. the tls pair for the secure old server [Rationale 7]
	# 2. the secure old server and the secure new server, the driver, both stopped
	# 3. the insecure old server and the insecure new server, the driver, both stopped
	# 1. an RSA key, because the old server signs tokens with it [master:restapi/configure_qdb_api_rest.go:245]
	# 2. secure first, because the login only means something there [Rationale 1]
	# 3. the new servers reuse REST_ARGS and REST_ARGS_SECURE, so the comparison and the round trip test one binary the same way
	# the cluster is seeded by make test-round-trip, which the target does not run [Rationale 4]
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
```

### `tests/e2e/compare-v1.sh`

```bash
# The v1 comparison (docs/e2e.md, The v1 comparison): one cluster per
# call. It logs in to the old server and the new one, then sends each
# query of QUERIES to both under each server's token. The old server is
# sent /api/<path>; the new server /api/v1/<path> and /api/<path>, and
# each of its answers is compared with the old server's. A 200 compares
# as bytes, anything else by status and the shape of the body.
#
#   compare-v1.sh --name <cluster> --old <url> --new <url> --table <name> --out <dir> [--login-body <file>]
#
#   style: file header, as stress.sh
#   - the Makefile is the only source of URLs and paths [tests/e2e/AGENTS.md]
#   - the table is the round trip's, already seeded [Rationale 4]
#   - no query sends #sql, and none exercises a listed deviation [Rationale 2, 3]

# QUERIES: the query texts, one per line, with @TABLE@ for the table.
# A select star over every type and null, no rows, a find that
# matches, a find that does not, an invalid query, an unknown table.
QUERIES=(...)
    # style: doc comment + inline notes
    # - no COUNT, because its type word is a listed deviation [docs/brief.md, Deliberate deviations]
    # - IN RANGE(1970, +1d), because no generated row is there [tests/e2e/tools/e2etool/main.go:53-55]
    # - the invalid query and the unknown table are the Go test's [internal/httpapi/v1/query_test.go:284-285]

# shape_of <body_file>: the body with every scalar replaced by its JSON
# type and every array cut to its first element, on stdout.
shape_of() { ... }   # style: doc comment alone

# compare <name> <old_status> <old_body> <new_status> <new_body>:
# statuses equal; then bytes equal on 200, shapes equal otherwise.
compare() {
    # style: doc comment + inline notes
    # - bytes on 200 and shape otherwise, because each server words its own errors [docs/log.md, M9 exit criterion 2]
    # - cmp -l head on a mismatch, because the bodies are one line and a diff shows nothing [proposal]
}

# login_case: the login on both servers, compared by shape and a
# non-empty token; sets OLD_TOKEN and NEW_TOKEN.
login_case() {
    # style: doc comment + inline notes
    # - the tokens differ by construction, so the shape and not the bytes [docs/brief.md, POST /api/v1/login]
    # - each server verifies only its own token [docs/brief.md, tokens minted by the old server are rejected]
}

# query_case <index> <query>: the query to the old server, then to the
# new server at both spellings, each new answer compared with the old.
query_case() {
    # style: doc comment + inline notes
    # - the old server knows the unversioned spelling only [ADR-0008, Consequences]
    # - the new server answers both, and each is compared with the old [ADR-0008, Consequences]
}

# run_cluster: login_case, then query_case over QUERIES.
run_cluster() { ... }   # style: doc comment alone

main() { parse_args; mkdir out; anonymous body when none; run_cluster || die; log passed }
```

### root `.gitignore`

```
# the old server's export, built by make old-server
tests/e2e/old-server/
```

### `docs/e2e.md`, after Stress definition

```markdown
## The v1 comparison

- what it is: the old server from `master` and the new one on the same
  cluster, a login then the queries to both, bytes on 200, status and
  shape otherwise, the token by shape (ADR-0015, D4)
- by hand, developer machines only, never Buildkite (owner, 2026-11-07)
- seeded by the round trip, which it does not run (owner, 2026-11-07)
- secure cluster first, then insecure (owner, 2026-11-07)
- the old server: export, qdb/ symlink, own .envrc; JSON config; the RSA
  TLS pair it signs tokens with
- the queries and what each proves; what is left out and why: #sql
  (owner, 2026-11-07), every listed deviation (brief), TZ=UTC on both
  so the UTC deviation never shows, gzip until v1 compresses
- Layout rows: compare-v1.sh, old-server/, the Makefile line
- Decision log (2026-11-07): export over worktree; one flow over cases
  per feature; seeded by the round trip
```

### `tests/e2e/AGENTS.md`

```markdown
- The v1 comparison (`compare-v1.sh`, `make test-v1-compare`) runs by
  hand and never in CI, after `make test-round-trip` has seeded the
  clusters. `make old-server` exports `master` into `old-server/` and
  builds it against the repository's `qdb/`; the export is never
  edited. The old server's config and TLS pair are written by
  `common.sh`. No query sends `#sql` and none exercises a deviation
  the brief lists.
```

### `tests/e2e/README.md`

```markdown
## The v1 comparison

- one paragraph in the shape of The stress: run the round trip first,
  what it starts, what it compares, by hand, working files
```

## Rationale

| decision                                                                                             | why                                                                                                      | rejected, and why                                                                                                    | gained                       | given up                                        | settled by                                                                                  |
| ---------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ---------------------------- | ----------------------------------------------- | ------------------------------------------------------------------------------------------- |
| 1. The secure cluster runs first, then the insecure one                                              | the login means something only on the secure cluster, and the anonymous login is the plugin's path       | secure only: the anonymous login would be compared with nothing                                                      | both logins compared         | two more servers per run                        | owner, 2026-11-07                                                                           |
| 2. No query sends `#sql`, and gzip waits for v1 to compress                                          | the owner excludes `#sql`; v1 answers identity today and the owner wants it to gzip first                | listing `#sql` as a deviation: the owner chose not to; comparing decoded gzip bodies: proves the plain case again    | nothing to explain           | both rest on the Go tests until then            | owner, 2026-11-07                                                                           |
| 3. No query exercises a listed deviation: wrong secret, `COUNT`, NaN, an old token, the process zone | the brief says no case of the comparison exercises an entry of the table                                 | comparing them and expecting a difference: proves nothing                                                            | every red is a bug           | the deviations rest on the Go tests             | `docs/brief.md`, Deliberate deviations                                                      |
| 4. One flow, a login then a query list, over a cluster the round trip has seeded                     | the owner asked for one login-then-query flow over seeded data, not a case per feature                   | the script generating and ingesting its own rows, a null table, token forms, a tag step: each a mechanism of its own | one script a reader can hold | the run depends on a prior round trip           | owner, 2026-11-07                                                                           |
| 5. The old server is built from a `git archive master` export, not a worktree                        | the export leaves `.git` untouched and its own `.envrc` works with a `qdb/` symlink; verified 2026-11-06 | `git worktree add`: registers in `.git`, needs pruning, and a stale one blocks a branch                              | one `rm -rf` cleans it       | a second checkout of `master` on disk           | proposal                                                                                    |
| 6. A separate driver and target, not steps of the round trip                                         | the round trip runs in CI on every platform and the comparison does not                                  | cases in `round-trip.sh` behind a flag: the CI driver would carry code it never runs                                 | the round trip unchanged     | a third driver                                  | proposal                                                                                    |
| 7. The old server gets a self-signed RSA pair written by the target                                  | it signs tokens with its TLS key, PKCS8 RSA                                                              | reusing the new server's ephemeral certificate: it is never written to disk                                          | no checked-in key            | `openssl` required                              | `master:restapi/configure_qdb_api_rest.go:245`, `master:qdbinterface/credentials.go:95-101` |
| 8. The old server is sent the unversioned path, the new server both spellings                        | the old server knows no other, and the v1 spelling must answer identically                               | the alias only: the canonical spelling would be untested against the old server                                      | both spellings proven        | three sends per query                           | ADR-0008, Consequences                                                                      |
| 9. A failed query compares by status and shape, through a jq type walk; a 200 as bytes               | each server words its own messages                                                                       | comparing messages: every message would differ                                                                       | shape proven                 | a message regression is not seen                | `docs/log.md`, M9 exit criterion 2; the jq walk is a proposal                               |
| 10. Both servers run under `TZ=UTC`                                                                  | `common.sh` exports it for every server it starts                                                        | the old server under a local zone to show the deviation: the brief leaves deviations out                             | timestamps compare as bytes  | the UTC deviation is never shown                | `tests/e2e/common.sh`; `docs/brief.md`, Deliberate deviations                               |
| 11. The old-server helpers go in `common.sh`, the compare in the driver                              | the Makefile sources `common.sh` and starts the servers; nothing else compares                           | everything in the driver: the Makefile could not start the old server                                                | the stress pattern kept      | `common.sh` carries two helpers one target uses | proposal                                                                                    |
| 12. The run stores nothing: working files under `actual/compare/`, cleaned by `make clean`           | validation runs on the owner's machine, and every stored golden would be one more file to audit          | a captured expected body: ADR-0015 removed them                                                                      | no goldens                   | a failure is reproduced by rerunning            | ADR-0015, Decision 4                                                                        |
| 13. Closing M9 is not a commit of this unit                                                          | a milestone state change is the owner's event in the log                                                 | a log line "criterion 2 green": a test outcome, which the log does not record                                        | the log stays the owner's    | the plan-stage message asks                     | `docs/AGENTS.md`, Entries                                                                   |

## Knowledge

- Commit 2: the Makefile comments of `old-server` (Rationale 5). The
  `.gitignore` line.
- Commit 3: `compare-v1.sh` header (Rationale 2, 3, 4, 6), `QUERIES`
  (Rationale 3 for `COUNT`), `shape_of`, `compare` (Rationale 9),
  `login_case` (brief, login), `query_case` (Rationale 8),
  `run_cluster`, `main` bare as in `stress.sh`.
- Commit 4: `old_config`, `old_tls_pair` in `common.sh` (Rationale 7,
  11), the `test-v1-compare` target's overview (Rationale 1, 4), both
  cluster blocks.
- Commit 5: `docs/e2e.md` The v1 comparison (Rationale 1, 2, 3, 4, 10,
  12; the decision log carries 5, 6, 11), Layout rows;
  `tests/e2e/AGENTS.md` bullet (5, 11); `README.md` section.
- Plan only: the gzip answer's note that v2 should compress everywhere
  (it seeds the other unit); the `/tmp` build of 2026-11-06; Rationale 13.
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
- Commit 4 is also the first byte comparison. A mismatch is a finding
  about v1 and not about the comparison; the executor reports it in
  the build-stage message with the first differing bytes and does not
  change the query to make it pass.
- The docs rows of commit 5 cite the owner's answers of 2026-11-07
  with their date.

## Commits

1. `docs(plan): v1-compare-plan.md, the side-by-side comparison of v1 with the old server` (this document)
2. `test(e2e): make old-server exports master and builds it against the repository's qdb`
3. `test(e2e): compare-v1.sh logs in to both servers and sends each query to both, bytes on 200, shape otherwise`
4. `test(e2e): make test-v1-compare writes the old server's tls pair and config and runs both clusters`
5. `docs(e2e): the v1 comparison in docs/e2e.md, tests/e2e/AGENTS.md and the README`
6. `/doc-discipline all tests/e2e docs/e2e.md docs/v1-compare-plan.md`, then `/doc-discipline check` against the plan; findings fixed as further commits
7. `docs(log): v1-compare-plan.md deleted, facts moved to docs/e2e.md, tests/e2e/AGENTS.md and the comments of tests/e2e` (Current state: Next item 1 removed, In flight none)
8. Verify: push `sc-19567/rr-v1-compare`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. Whether a green run closes M9 in the log. Recommendation: the owner
   closes M9 after the run, as a dated entry, outside this unit.
