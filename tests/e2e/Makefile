# qdb-api-rest e2e harness -- entry point (docs/e2e-plan.md).
# GNU make. Every path and port lives here and is passed down explicitly.
#
#   make load                       dataset into the running qdbd (idempotent)
#   make verify-dataset             export the loaded table, byte-compare with the CSV
#   make seed                       small legacy fixture tables + tags
#   make old-server                 build the old REST server from master
#   make capture-golden             (operator) capture goldens from the old server
#   make test-legacy-selfcheck      replay goldens against the old server
#   make test-legacy                replay goldens against the server under test
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
# Old-server flags (docs/bench-plan.md, "Server lifecycle"): --local pins
# localhost:40080; the buffer/pool sizes stop a 5.6M-row query from failing.
OLD_REST_ARGS  := --local -c $(QDB_URI) --pool-size 4 --parallelism-count 4 \
                  --max-in-buffer-size 8589934592 --log-file $(E2E_DIR)/old-rest-server.log

# Server under test: either a URL of a running server, or a binary the
# harness starts on REST_PORT with REST_ARGS. The default REST_ARGS match
# the flags of this repo's cmd/qdb_rest; the TLS listener is disabled so a
# test server never binds the deployment port 40443.
REST_URL     ?=
QDB_REST_BIN ?=
REST_PORT    ?= 40090
REST_ARGS    ?= -listen 127.0.0.1:$(REST_PORT) -listen-tls=
REST_PID     := $(E2E_DIR)/rest.pid
REST_LOG     := $(E2E_DIR)/rest.log

COMMON := source $(E2E_DIR)/common.sh
LEGACY := $(E2E_DIR)/legacy.sh
# Golden case selection for capture/replay targets; empty means all cases.
# Example: make test-legacy CASES='20-status-liveness 21-status-readiness'
CASES  ?=

.PHONY: help services-check download-golden extract load verify-dataset seed package-dataset \
        old-server capture-golden test-legacy test-legacy-selfcheck clean distclean

help:
	@sed -n '2,12p' $(firstword $(MAKEFILE_LIST))

services-check:
	@$(COMMON) && require_qdbd && log_info "qdbd answering on $(QDB_URI)"

# ------------------------------------------------------------------ dataset

# Fetch the archive named in datasets.json (or copy it from DATASETS_LOCAL_DIR)
# and verify its sha256. Idempotent: an existing verified archive is kept.
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

# Fidelity: export the loaded table and compare byte-for-byte with the CSV
# it was imported from (nulls, nanosecond timestamps, quoting).
verify-dataset: load
	@$(COMMON); mkdir -p $(E2E_DIR)/actual; \
	export_table_csv $(DATASET) $(E2E_DIR)/actual/$(DATASET).roundtrip.csv; \
	cmp $(E2E_DIR)/actual/$(DATASET).roundtrip.csv $(DATASET_DIR)/$(DATASET).csv \
	    && log_info "[OK] $(DATASET) round-trip is byte-identical" \
	    || die "[FAIL] $(DATASET) round-trip differs from $(DATASET_DIR)/$(DATASET).csv"

# Operator: db.tar.zst -> archive; prints the datasets.json entry and upload command.
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

capture-golden: services-check load seed
	@$(start_old_server); trap 'stop_server $(OLD_REST_PID)' EXIT; \
	$(LEGACY) capture $(OLD_REST_URL) $(CASES); \
	log_info "goldens captured; review with: git -C $(REPO_ROOT) status --short tests/e2e/golden"

test-legacy-selfcheck: services-check load seed
	@$(start_old_server); trap 'stop_server $(OLD_REST_PID)' EXIT; \
	$(LEGACY) replay $(OLD_REST_URL) $(CASES)

# ---------------------------------------------------------- server under test

test-legacy: services-check load seed
	@$(COMMON); \
	if [[ -n "$(REST_URL)" ]]; then $(LEGACY) replay "$(REST_URL)" $(CASES); exit $$?; fi; \
	[[ -n "$(QDB_REST_BIN)" ]] || die "no server under test: set QDB_REST_BIN=<binary> (M1) or REST_URL=<url>"; \
	start_server $(REST_PID) $(REST_LOG) $(REST_PORT) $(QDB_REST_BIN) $(REST_ARGS); \
	trap 'stop_server $(REST_PID)' EXIT; \
	$(LEGACY) replay http://127.0.0.1:$(REST_PORT) $(CASES)

# ------------------------------------------------------------------ cleanup

clean:
	@$(COMMON); stop_server $(OLD_REST_PID); stop_server $(REST_PID); \
	rm -rf $(E2E_DIR)/actual $(E2E_DIR)/*.log $(E2E_DIR)/*.pid $(DATASET_DIR)

distclean: clean
	@rm -rf $(DATASETS_DIR); \
	if [[ -d $(OLD_MASTER_DIR) ]]; then git -C $(REPO_ROOT) worktree remove --force $(OLD_MASTER_DIR); fi
