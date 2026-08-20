# qdb-api-rest assessment bench -- entry point (docs/bench-plan.md).
# GNU make, macOS developer machines only; never CI. This Makefile is the
# single source of truth for paths and ports; bench.py receives everything
# as explicit flags and has no defaults of its own.
#
#   make check                     C API parity between this repo and qdb-api-python
#   make venv                      bench venv; rebuilds the quasardb wheel on key change
#   make old-server                build the old REST server (delegates to tests/e2e)
#   make bench-native@qdbd         -> results/native@qdbd.json
#   make bench-legacy@old-rest     -> results/legacy@old-rest.json
#   make bench-legacy@new-rest     available with M1
#   make bench-flightsql@new-rest  available with M3
#   make report                    merge and print results/*.json
#
# qdbd and the dataset are the e2e harness's business:
#   bash scripts/tests/setup/start-services.sh && make -C tests/e2e load

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

BENCH_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
E2E_DIR   := $(abspath $(BENCH_DIR)/..)
REPO_ROOT := $(abspath $(E2E_DIR)/../..)

# The wheel is built with this interpreter and the venv must use the same one.
PYTHON             ?= /opt/local/bin/python3
QDB_API_PYTHON_DIR ?= $(HOME)/git/qdb-api-python

QDB_URI ?= qdb://127.0.0.1:2836
QDB_DIR ?= $(REPO_ROOT)/qdb

OLD_MASTER_DIR := $(E2E_DIR)/.old-master
OLD_REST_BIN   := $(OLD_MASTER_DIR)/apps/qdb_rest/qdb_rest
OLD_REST_PORT  := 40080
NEW_REST_BIN   ?=
NEW_REST_PORT  := 40090
FLIGHT_PORT    := 40493

VENV  := $(BENCH_DIR)/.env
STAMP := $(BENCH_DIR)/.stamp-wheel

REPS    ?= 3
QUERIES ?=

# The shipped rpath usually suffices; explicit library paths are cheap
# insurance for the quasardb extension inside the venv.
export DYLD_LIBRARY_PATH := $(QDB_DIR)/lib
export LD_LIBRARY_PATH   := $(QDB_DIR)/lib

# sha256 of the C API library a tree links against (dylib, else so).
# $(call capi_sha,<qdb tree>)
capi_sha = $$(shasum -a 256 $$(ls $(1)/lib/libqdb_api.dylib 2>/dev/null || ls $(1)/lib/libqdb_api.so) | awk '{print $$1}')

.PHONY: help check venv old-server report selftest clean distclean

help:
	@sed -n '6,17p' $(firstword $(MAKEFILE_LIST))

# Version purity (docs/bench-plan.md): the Python client and both REST
# servers must link the byte-identical C API.
check:
	@ours=$(call capi_sha,$(QDB_DIR)); theirs=$(call capi_sha,$(QDB_API_PYTHON_DIR)/qdb); \
	[[ "$$ours" == "$$theirs" ]] \
	    && echo "[OK] libqdb_api identical ($${ours:0:12}...)" \
	    || { echo "[FAIL] libqdb_api differs: $(QDB_DIR)=$$ours $(QDB_API_PYTHON_DIR)/qdb=$$theirs" >&2; \
	         echo "       re-run install-qdb to fan out one artifact tree" >&2; exit 1; }

# Bench venv. The quasardb wheel is built from the local qdb-api-python
# checkout via that repo's canonical build (slow C++), so it is cached on
# (qdb-api-python HEAD sha, C API sha256) in .stamp-wheel; the venv itself
# carries a copy of the stamp and is recreated whenever the wheel changes.
venv: check
	@key="$$(git -C $(QDB_API_PYTHON_DIR) rev-parse HEAD) $(call capi_sha,$(QDB_API_PYTHON_DIR)/qdb)"; \
	if [[ ! -f $(STAMP) || "$$(cat $(STAMP))" != "$$key" ]]; then \
	    echo "[INFO] building quasardb wheel (cache key changed)"; \
	    ( cd $(QDB_API_PYTHON_DIR) && PYTHON_CMD=$(PYTHON) CMAKE_GENERATOR=Ninja bash scripts/cicd/10.build.sh ); \
	    echo "$$key" > $(STAMP); \
	fi; \
	if [[ ! -f $(VENV)/.stamp-wheel || "$$(cat $(VENV)/.stamp-wheel)" != "$$key" ]]; then \
	    $(PYTHON) -m venv --clear $(VENV); \
	    $(VENV)/bin/pip install --quiet $(QDB_API_PYTHON_DIR)/dist/*.whl pandas; \
	    cp $(STAMP) $(VENV)/.stamp-wheel; \
	fi; \
	echo "[OK] venv ready: $(VENV)"

# One recipe for building the old server lives in tests/e2e.
old-server:
	@$(MAKE) -C $(E2E_DIR) old-server

# A run is one (protocol, server) pair: bench-native@qdbd, bench-legacy@old-rest, ...
bench-%: venv
	@cd $(BENCH_DIR) && $(VENV)/bin/python bench.py run --run '$*' \
	    --cluster $(QDB_URI) --qdb-dir $(QDB_DIR) \
	    --old-rest-bin $(OLD_REST_BIN) --old-rest-port $(OLD_REST_PORT) \
	    --new-rest-bin '$(NEW_REST_BIN)' --new-rest-port $(NEW_REST_PORT) \
	    --flight-port $(FLIGHT_PORT) \
	    --repo-root $(REPO_ROOT) --old-master-dir $(OLD_MASTER_DIR) \
	    --qdb-api-python-dir $(QDB_API_PYTHON_DIR) \
	    --reps $(REPS) $(if $(QUERIES),--queries '$(QUERIES)') \
	    --results-dir $(BENCH_DIR)/results

report: venv
	@cd $(BENCH_DIR) && $(VENV)/bin/python bench.py report --results-dir $(BENCH_DIR)/results

selftest: venv
	@cd $(BENCH_DIR) && $(VENV)/bin/python bench.py selftest

clean:
	@for pidfile in $(BENCH_DIR)/*.pid; do \
	    [[ -f "$$pidfile" ]] || continue; \
	    pid=$$(cat "$$pidfile"); kill "$$pid" 2>/dev/null || true; rm -f "$$pidfile"; \
	done; \
	rm -f $(BENCH_DIR)/results/*.json $(BENCH_DIR)/*.log

distclean: clean
	@rm -rf $(VENV) $(STAMP)
