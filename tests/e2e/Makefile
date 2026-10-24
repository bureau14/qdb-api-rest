# The entry point of the qdb-api-rest e2e harness (docs/e2e.md).
# It needs GNU make. Every path and port lives here and is passed down
# explicitly.
#
#   make e2etool                    build the Go tool of the v2 round trip into bin/
#   make test-round-trip            the v2 round trip, one server per cluster
#
# qdbd is a persistent service: bash scripts/tests/setup/start-services.sh

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

E2E_DIR   := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))

# The Windows agents check out to the root of a subst drive, where abspath
# answers Q:/ with its slash. The slash is stripped, because Q:/ joined with
# /qdb reaches common.sh's LD_LIBRARY_PATH as Q://qdb/lib, and the MSYS
# runtime resolves its //qdb/lib element as a network host every time bash
# starts a process, which costs seconds per process.
REPO_ROOT := $(patsubst %/,%,$(abspath $(E2E_DIR)/../..))

QDB_DIR   ?= $(REPO_ROOT)/qdb
QDB_URI   ?= qdb://127.0.0.1:2836
export QDB_DIR QDB_URI

# The server under test is either a URL of a running server, or a binary
# the harness starts on REST_PORT with REST_ARGS. The default REST_ARGS
# match the flags of this repo's cmd/qdb_rest. The HTTPS and Flight
# listeners cannot be turned off, so each takes a test port of its own:
# a test server never binds the deployment ports 40443 and 40493, and
# the two servers do not collide.
REST_URL     ?=
QDB_REST_BIN ?=
REST_PORT    ?= 40090
HTTPS_PORT   ?= 40092
FLIGHT_PORT  ?= 40094
REST_ARGS    ?= --listen-http 127.0.0.1:$(REST_PORT) \
                --listen-https 127.0.0.1:$(HTTPS_PORT) \
                --listen-flight 127.0.0.1:$(FLIGHT_PORT)
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
# their HTTPS and Flight listeners on test ports. The driver steps reach
# each server's Flight listener on its test port, and a server given as
# a URL is expected to serve Flight on that port too.
ROWS              ?= 10000
SEED              ?= $(shell date +%s)
REST_URL_INSECURE ?= $(REST_URL)
REST_URL_SECURE   ?=
QDB_URI_SECURE    := qdb://127.0.0.1:2838
REST_PORT_SECURE   ?= 40091
HTTPS_PORT_SECURE  ?= 40093
FLIGHT_PORT_SECURE ?= 40095
REST_ARGS_SECURE   ?= --cluster $(QDB_URI_SECURE) \
                      --cluster-public-key-file $(REPO_ROOT)/cluster_public.key \
                      --cluster-user-security-file $(REPO_ROOT)/user_private.key \
                      --listen-http 127.0.0.1:$(REST_PORT_SECURE) \
                      --listen-https 127.0.0.1:$(HTTPS_PORT_SECURE) \
                      --listen-flight 127.0.0.1:$(FLIGHT_PORT_SECURE)
REST_PID_SECURE   := $(E2E_DIR)/rest-secure.pid
REST_LOG_SECURE   := $(E2E_DIR)/rest-secure.log
ROUND_TRIP        := $(E2E_DIR)/round-trip.sh

COMMON := source $(E2E_DIR)/common.sh

.PHONY: help services-check e2etool test-round-trip clean

help:
	@sed -n '2,8p' $(firstword $(MAKEFILE_LIST))

services-check:
	@$(COMMON) && require_qdbd && log_info "qdbd answering on $(QDB_URI)"

# ------------------------------------------------------------ v2 round trip

# The tool is built with the server's toolchain and environment (the root
# .envrc, -mod=vendor) so it links the same C API as the server under
# test. -buildvcs=false, as in the CI build, because the build cannot
# stamp VCS inside the rhel7 builder.
e2etool:
	@$(COMMON); mkdir -p $(BIN_DIR); cd $(REPO_ROOT) && source ./.envrc && \
	    go build -mod=vendor -buildvcs=false -o $(E2ETOOL) ./tests/e2e/tools/e2etool && \
	    log_info "built $(E2ETOOL)"

# Both clusters must answer, because the round trip logs in to each.
# services-check probes the insecure one, and the secure probe lives
# here. The round trip loads nothing. Each server not given as a URL is
# started here and stopped on exit, one after the other being fine
# because the driver runs the clusters in sequence.
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
	$(ROUND_TRIP) run --insecure-url "$$insecure" --secure-url "$$secure" \
	    --insecure-flight 127.0.0.1:$(FLIGHT_PORT) --secure-flight 127.0.0.1:$(FLIGHT_PORT_SECURE) \
	    --tool $(E2ETOOL) --rows $(ROWS) --seed $(SEED)

# ------------------------------------------------------------------ cleanup

clean:
	@$(COMMON); stop_server $(REST_PID); stop_server $(REST_PID_SECURE); \
	rm -rf $(E2E_DIR)/actual $(E2E_DIR)/*.log $(E2E_DIR)/*.pid $(E2ETOOL)
