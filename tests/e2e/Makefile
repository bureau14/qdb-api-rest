# The entry point of the qdb-api-rest e2e harness (docs/e2e.md).
# It needs GNU make. Every path and port lives here and is passed down
# explicitly.
#
#   make load                       dataset into the running qdbd (idempotent)
#   make verify-dataset             export the loaded table, byte-compare with the CSV
#   make seed                       small fixture tables + tags
#   make old-server                 build the old REST server from master
#   make capture-v1                 (operator) capture goldens from the old server
#   make test-v1-selfcheck          replay goldens against the old server
#   make test-v1                    replay goldens against the server under test
#   make e2etool                    build the Go tool of the v2 round trip into bin/
#   make test-round-trip                  the v2 round trip, one server per cluster
#
# qdbd is a persistent service: bash scripts/tests/setup/start-services.sh

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

E2E_DIR   := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
REPO_ROOT := $(abspath $(E2E_DIR)/../..)

QDB_DIR   ?= $(REPO_ROOT)/qdb
QDB_URI   ?= qdb://127.0.0.1:2836
export QDB_DIR QDB_URI

DATASET        := reproduce
DATASET_ROWS   := 5613032
DATASETS_INDEX := $(E2E_DIR)/datasets.json
DATASETS_DIR   := $(E2E_DIR)/datasets
DATASET_DIR    := $(DATASETS_DIR)/$(DATASET)-$(DATASET_ROWS)
DATASETS_LOCAL_DIR ?=
IMPORT_JOBS    ?= 8

OLD_MASTER_DIR := $(E2E_DIR)/.old-master
OLD_REST_BIN   := $(OLD_MASTER_DIR)/apps/qdb_rest/qdb_rest
OLD_REST_PORT  := 40080
OLD_REST_URL   := http://127.0.0.1:$(OLD_REST_PORT)
OLD_REST_PID   := $(E2E_DIR)/old-rest.pid
OLD_REST_LOG   := $(E2E_DIR)/old-rest.log
# Old-server flags (docs/bench.md, "Server lifecycle"). --local pins
# localhost:40080, and the buffer/pool sizes stop a 5.6M-row query from
# failing.
OLD_REST_ARGS  := --local -c $(QDB_URI) --pool-size 4 --parallelism-count 4 \
                  --max-in-buffer-size 8589934592 --log-file $(E2E_DIR)/old-rest-server.log

# The server under test is either a URL of a running server, or a binary
# the harness starts on REST_PORT with REST_ARGS. The default REST_ARGS
# match the flags of this repo's cmd/qdb_rest, and the TLS listener is
# disabled so a test server never binds the deployment port 40443.
REST_URL     ?=
QDB_REST_BIN ?=
REST_PORT    ?= 40090
REST_ARGS    ?= --listen-http 127.0.0.1:$(REST_PORT) --listen-https=
REST_PID     := $(E2E_DIR)/rest.pid
REST_LOG     := $(E2E_DIR)/rest.log

# The Go tool of the v2 round trip (docs/e2e.md, "The tool"). It is built
# into the repository's bin/, next to qdb_rest, so every cgo binary the
# round trip runs sits in one directory and the CI step's Windows DLL
# co-location is one copy (scripts/cicd/40.test-e2e.sh). It gains .exe
# on Windows like every binary the harness runs (common.sh's EXE_EXT,
# which is a shell variable, so make derives its own).
EXE_EXT := $(if $(filter MINGW% MSYS% CYGWIN%,$(shell uname -s)),.exe,)
BIN_DIR := $(REPO_ROOT)/bin
E2ETOOL := $(BIN_DIR)/e2etool$(EXE_EXT)

# The v2 round trip (docs/e2e.md, "The v2 round trip") runs on both clusters, each
# against its own server, because the secure one costs one block here
# and is the only step that proves the key-file flags. ROWS and SEED
# select the generated data, and the driver prints both, so a failure
# reproduces. A cluster whose URL is given is used as it is. Otherwise
# its server is started from QDB_REST_BIN. The secure server takes the
# key files start-services.sh writes at the repo root, and both run
# with the TLS listener off.
ROWS              ?= 10000
SEED              ?= $(shell date +%s)
REST_URL_INSECURE ?= $(REST_URL)
REST_URL_SECURE   ?=
QDB_URI_SECURE    := qdb://127.0.0.1:2838
REST_PORT_SECURE  ?= 40091
REST_ARGS_SECURE  ?= --cluster $(QDB_URI_SECURE) \
                     --cluster-public-key-file $(REPO_ROOT)/cluster_public.key \
                     --cluster-user-security-file $(REPO_ROOT)/user_private.key \
                     --listen-http 127.0.0.1:$(REST_PORT_SECURE) --listen-https=
REST_PID_SECURE   := $(E2E_DIR)/rest-secure.pid
REST_LOG_SECURE   := $(E2E_DIR)/rest-secure.log
ROUND_TRIP        := $(E2E_DIR)/round-trip.sh

COMMON := source $(E2E_DIR)/common.sh
GOLDEN := $(E2E_DIR)/golden.sh
# Golden case selection for capture/replay targets. Empty means all cases.
# Example: make test-v1 CASES='01-login-anonymous 08-query-seed-select'
CASES  ?=

.PHONY: help services-check download-golden extract load verify-dataset seed package-dataset \
        old-server capture-v1 test-v1 test-v1-selfcheck e2etool test-round-trip clean distclean

help:
	@sed -n '2,14p' $(firstword $(MAKEFILE_LIST))

services-check:
	@$(COMMON) && require_qdbd && log_info "qdbd answering on $(QDB_URI)"

# ------------------------------------------------------------------ dataset

# Fetch the archive named in datasets.json (or copy it from DATASETS_LOCAL_DIR)
# and verify its sha256. It is idempotent, because an existing verified
# archive is kept.
download-golden:
	@$(COMMON) && require_command jq && require_command curl; \
	entry=$$(jq -c --arg n "$(DATASET)" --argjson r $(DATASET_ROWS) \
	    '.archives[] | select(.name == $$n and .rows == $$r)' $(DATASETS_INDEX)); \
	[[ -n "$$entry" ]] || die "no datasets.json entry for $(DATASET)-$(DATASET_ROWS)"; \
	date=$$(jq -r .date <<<"$$entry"); sha=$$(jq -r .sha256 <<<"$$entry"); \
	base_url=$$(jq -r .base_url $(DATASETS_INDEX)); \
	archive=$(DATASET)-$$date-$(DATASET_ROWS).tar.gz; dest=$(DATASETS_DIR)/$$archive; \
	mkdir -p $(DATASETS_DIR); \
	if [[ -f "$$dest" && "$$(sha256_file "$$dest")" == "$$sha" ]]; then \
	    log_info "archive present and verified: $$archive"; exit 0; fi; \
	rm -f "$$dest"; \
	if [[ -n "$(DATASETS_LOCAL_DIR)" && -f "$(DATASETS_LOCAL_DIR)/$$archive" ]]; then \
	    log_info "copying $$archive from $(DATASETS_LOCAL_DIR)"; cp "$(DATASETS_LOCAL_DIR)/$$archive" "$$dest"; \
	else \
	    log_info "downloading $$base_url/$$archive"; \
	    curl -fL -o "$$dest" "$$base_url/$$archive" || { rm -f "$$dest"; die "download failed: $$archive"; }; \
	fi; \
	actual=$$(sha256_file "$$dest"); \
	[[ "$$actual" == "$$sha" ]] || { rm -f "$$dest"; die "sha256 mismatch for $$archive: got $$actual, want $$sha"; }; \
	log_info "verified $$archive"

extract: download-golden
	@$(COMMON); \
	if [[ -f $(DATASET_DIR)/.extracted ]]; then log_info "already extracted: $(DATASET_DIR)"; exit 0; fi; \
	archives=( $(DATASETS_DIR)/$(DATASET)-*-$(DATASET_ROWS).tar.gz ); \
	[[ $${#archives[@]} -eq 1 && -f "$${archives[0]}" ]] || die "expected exactly one archive, got: $${archives[*]}"; \
	mkdir -p $(DATASET_DIR); tar -xzf "$${archives[0]}" -C $(DATASET_DIR); touch $(DATASET_DIR)/.extracted; \
	log_info "extracted $${archives[0]}"

# Load the dataset into the running qdbd unless it is already there.
load: services-check extract
	@$(COMMON); \
	have=$$(count_qdb_rows $(DATASET)); \
	if [[ "$$have" == "$(DATASET_ROWS)" ]]; then log_info "$(DATASET) already loaded ($$have rows)"; exit 0; fi; \
	[[ "$$have" == "0" ]] || die "$(DATASET) has $$have rows, expected 0 or $(DATASET_ROWS); drop it and retry"; \
	log_info "importing $(DATASET) ($(DATASET_ROWS) rows, $(IMPORT_JOBS) jobs)"; \
	"$$QDB_IMPORT" -f $(DATASET_DIR)/$(DATASET).csv --config $(DATASET_DIR)/$(DATASET).import.json \
	    -j $(IMPORT_JOBS) -c $(QDB_URI) --log-level warning; \
	have=$$(count_qdb_rows $(DATASET)); \
	[[ "$$have" == "$(DATASET_ROWS)" ]] || die "import ended with $$have rows, expected $(DATASET_ROWS)"; \
	log_info "$(DATASET) loaded ($$have rows)"

# Fidelity check. Export the loaded table and compare it byte for byte
# with the CSV it was imported from (nulls, nanosecond timestamps, quoting).
verify-dataset: load
	@$(COMMON); mkdir -p $(E2E_DIR)/actual; \
	export_table_csv $(DATASET) $(E2E_DIR)/actual/$(DATASET).roundtrip.csv; \
	cmp $(E2E_DIR)/actual/$(DATASET).roundtrip.csv $(DATASET_DIR)/$(DATASET).csv \
	    && log_info "[OK] $(DATASET) round-trip is byte-identical" \
	    || die "[FAIL] $(DATASET) round-trip differs from $(DATASET_DIR)/$(DATASET).csv"

# Operator step. It turns db.tar.zst into the archive and prints the
# datasets.json entry and the upload command.
package-dataset:
	@[[ -n "$(SRC)" && -n "$(OUT)" ]] || { echo "usage: make package-dataset SRC=<db.tar.zst> OUT=<dir>"; exit 1; }
	@$(E2E_DIR)/tools/package-dataset.sh "$(SRC)" "$(OUT)"

# ------------------------------------------------------------------ fixture

seed: services-check
	@$(COMMON) && run_qdbsh_file $(E2E_DIR)/seed.sql && log_info "fixture seeded"

# --------------------------------------------------------------- old server

# Build the old REST server from master in a worktree, linked against this
# repo's qdb/ tree (same C API as the server under test).
old-server:
	@$(COMMON); \
	git -C $(REPO_ROOT) worktree prune; \
	if [[ ! -d $(OLD_MASTER_DIR) ]]; then git -C $(REPO_ROOT) worktree add -q --detach $(OLD_MASTER_DIR) master; fi; \
	ln -sfn $(QDB_DIR) $(OLD_MASTER_DIR)/qdb; \
	cd $(OLD_MASTER_DIR)/apps/qdb_rest && source ../../.envrc && \
	    go build -mod=vendor -buildvcs=false -o qdb_rest . && \
	log_info "built $(OLD_REST_BIN)"

define start_old_server
	$(COMMON); [[ -x $(OLD_REST_BIN) ]] || die "old server not built; run: make old-server"; \
	start_server $(OLD_REST_PID) $(OLD_REST_LOG) $(OLD_REST_PORT) $(OLD_REST_BIN) $(OLD_REST_ARGS)
endef

capture-v1: services-check load seed
	@$(start_old_server); trap 'stop_server $(OLD_REST_PID)' EXIT; \
	$(GOLDEN) capture $(OLD_REST_URL) $(CASES); \
	log_info "goldens captured; review with: git -C $(REPO_ROOT) status --short tests/e2e/golden"

test-v1-selfcheck: services-check load seed
	@$(start_old_server); trap 'stop_server $(OLD_REST_PID)' EXIT; \
	$(GOLDEN) replay $(OLD_REST_URL) $(CASES)

# ---------------------------------------------------------- server under test

test-v1: services-check load seed
	@$(COMMON); \
	if [[ -n "$(REST_URL)" ]]; then $(GOLDEN) replay "$(REST_URL)" $(CASES); exit $$?; fi; \
	[[ -n "$(QDB_REST_BIN)" ]] || die "no server under test: set QDB_REST_BIN=<binary> or REST_URL=<url>"; \
	start_server $(REST_PID) $(REST_LOG) $(REST_PORT) $(QDB_REST_BIN) $(REST_ARGS); \
	trap 'stop_server $(REST_PID)' EXIT; \
	$(GOLDEN) replay http://127.0.0.1:$(REST_PORT) $(CASES)

# ------------------------------------------------------------ v2 round trip

# The tool is built with the server's toolchain and environment (the root
# .envrc, -mod=vendor) so it links the same C API as the server under
# test. -buildvcs=false, as in the CI build, because the build cannot
# stamp VCS inside the rhel7 builder.
e2etool:
	@$(COMMON); mkdir -p $(BIN_DIR); cd $(REPO_ROOT) && source ./.envrc && \
	    go build -mod=vendor -buildvcs=false -o $(E2ETOOL) ./tests/e2e/tools/e2etool && \
	    log_info "built $(E2ETOOL)"

# Both clusters must answer, because the round trip logs in to each. The v1
# targets need the insecure one alone, so services-check keeps its one
# port and the secure probe lives here. The round trip loads nothing. Each
# server not given as a URL is started here and stopped on exit, one
# after the other being fine because the driver runs the clusters in
# sequence.
test-round-trip: services-check e2etool
	@$(COMMON); \
	check_service_running qdbd $(subst qdb://127.0.0.1:,,$(QDB_URI_SECURE)) \
	    || die "secure qdbd is not answering on $(QDB_URI_SECURE); run: bash scripts/tests/setup/start-services.sh"; \
	insecure="$(REST_URL_INSECURE)"; secure="$(REST_URL_SECURE)"; \
	if [[ -z "$$insecure" || -z "$$secure" ]]; then \
	    [[ -n "$(QDB_REST_BIN)" ]] || die "no server under test: set QDB_REST_BIN=<binary> or REST_URL_INSECURE and REST_URL_SECURE"; \
	fi; \
	trap 'stop_server $(REST_PID); stop_server $(REST_PID_SECURE)' EXIT; \
	if [[ -z "$$insecure" ]]; then \
	    start_server $(REST_PID) $(REST_LOG) $(REST_PORT) $(QDB_REST_BIN) $(REST_ARGS); \
	    insecure=http://127.0.0.1:$(REST_PORT); \
	fi; \
	if [[ -z "$$secure" ]]; then \
	    start_server $(REST_PID_SECURE) $(REST_LOG_SECURE) $(REST_PORT_SECURE) $(QDB_REST_BIN) $(REST_ARGS_SECURE); \
	    secure=http://127.0.0.1:$(REST_PORT_SECURE); \
	fi; \
	$(ROUND_TRIP) run --insecure-url "$$insecure" --secure-url "$$secure" --tool $(E2ETOOL) --rows $(ROWS) --seed $(SEED)

# ------------------------------------------------------------------ cleanup

clean:
	@$(COMMON); stop_server $(OLD_REST_PID); stop_server $(REST_PID); stop_server $(REST_PID_SECURE); \
	rm -rf $(E2E_DIR)/actual $(E2E_DIR)/*.log $(E2E_DIR)/*.pid $(DATASET_DIR) $(E2ETOOL)

distclean: clean
	@rm -rf $(DATASETS_DIR); \
	if [[ -d $(OLD_MASTER_DIR) ]]; then git -C $(REPO_ROOT) worktree remove --force $(OLD_MASTER_DIR); fi
