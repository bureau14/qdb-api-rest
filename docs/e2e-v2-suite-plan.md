# The v2 golden suite, first slice -- Plan

Status: approved. Scaffolding for one unit of work: `golden.sh` learns
the v2 suite and the seed-fixture cases land on both clusters. The
harness it extends is specified in `docs/e2e-plan.md` (Goldens, "The
v2 suite"); the decisions it honors are ADR-0010, ADR-0011, ADR-0012 and
ADR-0013. Deleted when the slice merges; anything permanent moves to
`docs/e2e-plan.md`, `tests/e2e/AGENTS.md` or `tests/e2e/README.md`
first (`docs/AGENTS.md`, Plans).

## Scope

In: the driver generalized to a suite argument; a v2 query case run as
a formats x encodings matrix over the text formats (`json`, `ndjson`,
`csv`) under `identity` and `gzip`; a case naming its cluster; the seed
on both nodes; `capture-v2` and `test-v2` driving one server per
cluster; the `request.json` of every case the seed fixture can carry;
the stale comments in `tests/e2e/`; the documentation of the operator
capture cycle.

The oversized-body (413) case is not a golden: every reference to it
leaves `docs/e2e-plan.md` in this slice (owner decision 2026-09-18: the
friction of a generated body outweighs a row `readBody` already pins in
Go).

## The case format

`tests/e2e/golden/<suite>/<case>/request.json`, hand-written:

| field       | v1                                 | v2                                                                        |
| ----------- | ---------------------------------- | ------------------------------------------------------------------------- |
| `method`    | `POST`                             | `POST`                                                                    |
| `path`      | `/api/login`, `/api/query`         | `/api/v2/auth/login`, `/api/v2/query`                                     |
| `query`     | pre-encoded query string, optional | same                                                                      |
| `headers`   | request headers                    | same                                                                      |
| `body`      | a JSON value, sent compact         | an object is sent compact; a string is sent as its text (the query body)  |
| `auth`      | `none`, `bearer`, `urlparam`       | `none`, `bearer` (v2 never carries a token in a URL, ADR-0010)            |
| `compare`   | `bytes`, `gunzip`, `login-shape`   | `bytes`, `login-shape`; a query case has no `compare`, the matrix decides |
| `cluster`   | absent, `insecure`                 | `insecure`, `secure`, `both`; default `both`                              |
| `formats`   | absent                             | a query case: subset of `json`, `ndjson`, `csv`                           |
| `encodings` | absent                             | a query case: subset of `identity`, `gzip`; absent means `identity`       |

Captured files, written only by `make capture-<suite>`:

- `status`: three digits and a newline, one per case, shared by every
  run of a matrix.
- `headers`: `content-type` and `content-encoding` only, lowercased,
  sorted, absence recorded as absence -- the v1 rule, kept for both
  suites (owner decision 2026-09-18: the body and the status are the
  assertion; `WWW-Authenticate`, `Vary`, `Retry-After` and
  `X-Request-Id` are never stored). A matrix case stores no `headers`
  file: `content-type` is asserted from the format and
  `content-encoding` from the encoding of each run.
- `body` for a single-run case; `body.json`, `body.ndjson`, `body.csv`
  for a matrix case, one per format, shared by every encoding.

A `both` case runs once per cluster against the same golden files; a
query answers the same bytes on both nodes because the seed and the
encoder are the same. Replay writes to `actual/<suite>/<case>/<cluster>/`.

## The login per suite

One token per (suite, cluster), fetched lazily by the first case that
needs one (`docs/e2e-plan.md`, decision log 2026-08-20). v1 logs in
anonymously at `/api/login` and reads `.token`; v2 logs in at
`/api/v2/auth/login` and reads `.access_token`. On the secure cluster
the login body is the secure user's `{username, secret_key}`, read from
the user security file `start-services.sh` writes at the repo root
(`user_private.key`, the file `internal/qdbtest` reads too); on the
insecure cluster it is the anonymous body.

`login-shape` per suite: v1 `{"token": <non-empty string>}`; v2
`access_token` a non-empty string, `token_type` `Bearer`, `expires_in`
a positive integer (RFC 6749; ADR-0011).

## Two environments

`make seed` runs `seed.sql` against the insecure node and against the
secure node; the statements are identical, the secure qdbsh call adds
`--cluster-public-key-file` and `--user-security-file`. `reproduce` is
loaded on the insecure node only; no case in this slice reads it, and
the `reproduce` case of a later slice is `cluster: insecure`.

`capture-v2` and `test-v2` start two servers under test from
`QDB_REST_BIN`: `40090` on `qdb://127.0.0.1:2836`, `40091` on
`qdb://127.0.0.1:2838` with `--cluster-public-key-file` and
`--cluster-user-security-file`, TLS listener off, `TZ=UTC`. A running
server is named explicitly per cluster, `REST_URL_INSECURE` and
`REST_URL_SECURE`; a cluster whose variable is empty gets a server
started from `QDB_REST_BIN`, so one, both or neither may be set.
`test-v1` takes its one server from `REST_URL_INSECURE`; the bare
`REST_URL` leaves.

## Cases

Numbering: three digits, the first says what a case is -- `0xx` logins,
`1xx` queries, `2xx` errors (owner decision 2026-09-18; v1 keeps its
two-digit names). Audit source per case as `docs/e2e-plan.md` requires:
a data case against qdbsh output for the same query, a shape or error
case against the ADR that owns it.

| case                               | cluster  | compare / matrix                   | audit source                                                                                         |
| ---------------------------------- | -------- | ---------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `001-login-anonymous`              | insecure | `login-shape`                      | ADR-0011 3                                                                                           |
| `002-login-secure-user`            | secure   | `login-shape`                      | ADR-0011 3                                                                                           |
| `003-login-refused-credentials`    | secure   | `bytes` (401 problem)              | ADR-0011 5; the detail is the binding's message                                                      |
| `100-query-seed-types`             | both     | json, ndjson, csv x identity, gzip | `qdbsh -c 'SELECT * FROM seed_types'`                                                                |
| `101-query-seed-allnull`           | both     | json, ndjson, csv x identity       | `qdbsh -c 'SELECT i FROM seed_allnull'`; type `int64`                                                |
| `102-query-empty-result`           | both     | json, ndjson, csv x identity       | `SELECT * FROM foo_01 IN RANGE (1990, 1991)`; zero rows per `internal/encoding/AGENTS.md`, Rendering |
| `200-query-no-bearer`              | both     | `bytes`                            | ADR-0010 7, "no bearer"                                                                              |
| `201-query-malformed-bearer`       | both     | `bytes`                            | ADR-0010 7, "bad bearer"; `Authorization: Bearer garbage`                                            |
| `202-query-unsupported-media-type` | both     | `bytes`                            | ADR-0010 1, 7; `Content-Type: application/json`                                                      |
| `203-query-invalid`                | both     | `bytes`                            | ADR-0010 7, "the cluster answered"; `SELECT FROM`                                                    |

The invalid-query and refused-credentials bodies pin the binding's
error text, as v1's `14-query-syntax-error` already does; a C API
message change fails them by design (ADR-0013, Consequences).

## Operator capture cycle

The rule that goes to `tests/e2e/AGENTS.md`, stated once here so the
owner reviews the wording:

- Every golden is captured and audited by an operator, never by CI and
  never by an agent: `make capture-<suite>`, then the audit above, then
  a commit of the captured files as-is.
- A body small enough to review lives in git next to its
  `request.json`; a body too large for git lives in the dated dataset
  archive under `expected/<suite>/<case>/` (`datasets.json`, one entry
  per archive version), so a recapture of a large body is a new archive
  version, uploaded by the operator.
- A deliberate wire change -- a new feature, a changed encoder --
  fails the goldens; the operator repeats the cycle, and the recapture
  is reviewed as a diff against the previous goldens.

## Stale comments

Found by the sweep of `tests/e2e/` (owner decision 2026-09-18: all are
addressed):

- `tests/e2e/common.sh:6-7`: the header lists a "CSV comparison"
  section that left with `compare_csv`.
- `tests/e2e/seed.sql:52`: "both null sentinels" names strings the
  wire never carries; the rows are one all-null and one mixed-null.
- `tests/e2e/golden.sh:2-19`: the header is v1-only; rewritten by the
  first commit, not a separate fix.

## Commits

1. `refactor(e2e): golden.sh takes the suite; the v1 targets pass it`
2. `feat(e2e): the token comes from the suite's own login, one per cluster`
3. `feat(e2e): a string body is sent as text, an object as compact json`
4. `feat(e2e): login-shape checks the rfc 6749 fields on v2`
5. `feat(e2e): a case names its cluster: insecure, secure or both`
6. `feat(e2e): a v2 query case runs its formats and encodings as a matrix`
7. `feat(e2e): seed runs against the secure node too`
8. `feat(e2e): capture-v2 and test-v2 drive one server per cluster`
9. `test(e2e): v2 login requests: anonymous, the secure user, refused credentials`
10. `test(e2e): v2 query requests: seed_types, seed_allnull, empty result, on both clusters`
11. `test(e2e): v2 error requests: no bearer, malformed bearer, unsupported media type, invalid query`

Checkpoint, owner: `make -C tests/e2e capture-v2 QDB_REST_BIN=bin/qdb_rest`
(runs `services-check load seed` first; both nodes up, `reproduce`
loaded or imported now), audit each case against its source above,
`git add tests/e2e/golden/v2 && git commit -m "test(e2e): v2 goldens captured"`,
then say continue.

12. `fix(e2e): common.sh's header names what the file holds; seed.sql's null rows`
13. `docs(e2e): AGENTS.md and README cover the v2 suite and the operator capture cycle`
14. `docs(e2e-plan): the two stored headers, the cluster field; the oversized-body case leaves`
15. `docs(log): the v2 driver and seed cases landed; arrow, the archive and buildkite remain`

Every commit builds and passes `make test-v1-selfcheck` (the v1 suite
through the generalized driver) where it touches the driver; after the
checkpoint, `make test-v2 QDB_REST_BIN=bin/qdb_rest` is green.

## Open questions and recommendations

1. `102-query-empty-result`: what zero rows look like per format is the
   encoder's rule (`internal/encoding/AGENTS.md`, Rendering: JSON keeps
   the columns with empty `data`, NDJSON is an empty body, CSV the
   header alone); the golden pins it, no decision needed. Kept in the
   table as a reminder for the audit.
2. Port of the secure server under test: `40091`. Distinct from every
   port the bench uses (`docs/bench-plan.md`, Layout); a Makefile
   variable like `REST_PORT`.

## Decision log (2026-09-18)

| Decision                                            | Why                                                                           | Rejected                                                 |
| --------------------------------------------------- | ----------------------------------------------------------------------------- | -------------------------------------------------------- |
| Two stored headers for both suites                  | the body and the status are the assertion; the rest is noise a client ignores | storing `www-authenticate`, `vary`, `retry-after` for v2 |
| No 413 case                                         | a generated 1 MiB body is driver friction for a row the Go test pins          | a `body_repeat` field                                    |
| `cluster: insecure \| secure \| both`, default both | the same case on two parallel environments; only the login differs            | separate secure cases; a second suite                    |
| Three-digit case names, first digit the kind        | room for growth; sorts; the kind is readable                                  | renumbering v1; two-digit blocks                         |
| The operator captures                               | a golden is an audited response; the audit is a human judgment                | the agent captures and the owner reviews the diff        |
| `REST_URL_INSECURE` and `REST_URL_SECURE`           | explicit per cluster; no case is silently skipped                             | one `REST_URL` that skips the `secure` cases             |
