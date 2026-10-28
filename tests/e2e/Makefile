# The entry point of the qdb-api-rest e2e harness (docs/e2e.md).
# It needs GNU make. Every path and port lives here and is passed down
# explicitly.
#
#   make e2etool                    build the Go tool of the v2 round trip into bin/
#   make jdbc                       build the JDBC smoke jar of the v2 round trip
#   make test-round-trip            the v2 round trip, one server per cluster
#   make test-stress                the stress, by hand: the drain step on both clusters
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

# The JDBC smoke step's jar (docs/e2e.md, The Flight SQL smoke steps),
# which Maven builds in the project's own target/.
JDBC_DIR := $(E2E_DIR)/tools/jdbc
JDBC_JAR := $(JDBC_DIR)/target/jdbc-smoke.jar

# The v2 round trip (docs/e2e.md, "The v2 round trip") runs on both clusters, each
# against its own server, because the secure one costs one block here
# and is the only step that proves the key-file flags. ROWS and SEED
# select the generated data, and the driver prints both, so a failure
# reproduces. A cluster whose URL is given is used as it is. Otherwise
# its server is started from QDB_REST_BIN. The secure server takes the
# key files start-services.sh writes at the repo root, and both run
# their HTTPS and Flight listeners on test ports. Each plane the driver
# reads is a URL in the form its client takes, http:// for curl and
# grpc+tls:// for the ADBC driver, and a server given by its URLs is used
# as it is, Flight included.
ROWS                ?= 10000
SEED                ?= $(shell date +%s)
REST_URL_INSECURE   ?= $(REST_URL)
REST_URL_SECURE     ?=
FLIGHT_URL_INSECURE ?= grpc+tls://127.0.0.1:$(FLIGHT_PORT)
FLIGHT_URL_SECURE   ?= grpc+tls://127.0.0.1:$(FLIGHT_PORT_SECURE)
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

.PHONY: help services-check e2etool jdbc test-round-trip test-stress clean

help:
	@sed -n '2,10p' $(firstword $(MAKEFILE_LIST))

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

# Maven resolves the driver into its local repository and shades it with
# the tool into one jar. A missing java or mvn fails the target, and
# with it the round trip, on every platform, because a skip written
# before the failure is seen hides where it should not apply.
jdbc:
	@$(COMMON); require_command java; require_command mvn; \
	    cd $(JDBC_DIR) && mvn -q package && log_info "built $(JDBC_JAR)"

# Both clusters must answer, because the round trip logs in to each.
# services-check probes the insecure one, and the secure probe lives
# here. The round trip loads nothing. Each server not given as a URL is
# started here and stopped on exit, one after the other being fine
# because the driver runs the clusters in sequence. Each server started
# here passes its log file to the driver, and a server given as a URL
# passes none.
test-round-trip: services-check e2etool jdbc
	@$(COMMON); \
	check_service_running qdbd $(subst qdb://127.0.0.1:,,$(QDB_URI_SECURE)) \
	    || die "secure qdbd is not answering on $(QDB_URI_SECURE); run: bash scripts/tests/setup/start-services.sh"; \
	insecure="$(REST_URL_INSECURE)"; secure="$(REST_URL_SECURE)"; \
	insecure_log=""; secure_log=""; \
	if [[ -z "$$insecure" || -z "$$secure" ]]; then \
	    [[ -n "$(QDB_REST_BIN)" ]] || die "no server under test: set QDB_REST_BIN=<binary> or REST_URL_INSECURE and REST_URL_SECURE"; \
	fi; \
	trap 'stop_server $(REST_PID); stop_server $(REST_PID_SECURE)' EXIT; \
	if [[ -z "$$insecure" ]]; then \
	    start_server $(REST_PID) $(REST_LOG) $(REST_PORT) $(QDB_REST_BIN) $(REST_ARGS); \
	    insecure=http://127.0.0.1:$(REST_PORT); insecure_log=$(REST_LOG); \
	fi; \
	if [[ -z "$$secure" ]]; then \
	    start_server $(REST_PID_SECURE) $(REST_LOG_SECURE) $(REST_PORT_SECURE) $(QDB_REST_BIN) $(REST_ARGS_SECURE); \
	    secure=http://127.0.0.1:$(REST_PORT_SECURE); secure_log=$(REST_LOG_SECURE); \
	fi; \
	$(ROUND_TRIP) run --insecure-http "$$insecure" --insecure-flight $(FLIGHT_URL_INSECURE) \
	    --secure-http "$$secure" --secure-flight $(FLIGHT_URL_SECURE) \
	    --tool $(E2ETOOL) --jdbc-jar $(JDBC_JAR) --rows $(ROWS) --seed $(SEED) \
	    --insecure-log "$$insecure_log" --secure-log "$$secure_log"

# ---------------------------------------------------------------- stress

# The stress (docs/e2e.md, Stress definition) runs by hand and never in
# CI. Its parameters live here. STRESS_ROWS is sized so the CSV body
# outgrows the loopback socket buffers and curl's own, so that the
# server's write blocks on the slow client and the stream is in flight
# when the signal arrives; not measured, the first value that held.
# STRESS_LIMIT_RATE makes the read outlast the signal by seconds.
# STRESS_GRACE outlasts the read by a wide margin, because a grace that
# expires cuts the stream by process exit and fails the body assertion,
# and STRESS_EXIT_WINDOW sits above the grace. The budget is two
# sessions, the smallest that leaves the ingest and the read one each.
STRESS_ROWS        ?= 200000
STRESS_SEED        ?= $(shell date +%s)
STRESS_LIMIT_RATE  ?= 500k
STRESS_GRACE       ?= 60s
STRESS_EXIT_WINDOW ?= 70
STRESS_FLAGS       := --listen-shutdown-grace $(STRESS_GRACE) --pool-max-sessions 2 --pool-per-user-max 2
STRESS_PID         := $(E2E_DIR)/stress.pid
STRESS_LOG         := $(E2E_DIR)/stress.log
STRESS_PID_SECURE  := $(E2E_DIR)/stress-secure.pid
STRESS_LOG_SECURE  := $(E2E_DIR)/stress-secure.log
STRESS_OUT         := $(E2E_DIR)/actual/stress
STRESS             := $(E2E_DIR)/stress.sh

# The stress always starts its servers, because the step ends the server
# with SIGTERM, so no URL form exists. The round trip's ports are reused,
# because the two suites never run at once on one machine, and a clash
# fails the bind loudly. Own pidfiles and logs, because the log is
# asserted. One cluster after the other, each server started here and
# ended by the step, which waits for the exit itself; the trap covers a
# failed step, where stop_server finds a dead pid or a server it may
# kill.
test-stress: services-check e2etool
	@$(COMMON); \
	check_service_running qdbd $(subst qdb://127.0.0.1:,,$(QDB_URI_SECURE)) \
	    || die "secure qdbd is not answering on $(QDB_URI_SECURE); run: bash scripts/tests/setup/start-services.sh"; \
	[[ -n "$(QDB_REST_BIN)" ]] || die "no server under test: set QDB_REST_BIN=<binary>"; \
	trap 'stop_server $(STRESS_PID); stop_server $(STRESS_PID_SECURE)' EXIT; \
	rm -rf $(STRESS_OUT); \
	start_server $(STRESS_PID) $(STRESS_LOG) $(REST_PORT) $(QDB_REST_BIN) $(REST_ARGS) $(STRESS_FLAGS); \
	$(STRESS) drain --name insecure --http http://127.0.0.1:$(REST_PORT) --pid-file $(STRESS_PID) --log $(STRESS_LOG) \
	    --tool $(E2ETOOL) --out $(STRESS_OUT)/insecure --rows $(STRESS_ROWS) --seed $(STRESS_SEED) \
	    --limit-rate $(STRESS_LIMIT_RATE) --exit-window $(STRESS_EXIT_WINDOW); \
	start_server $(STRESS_PID_SECURE) $(STRESS_LOG_SECURE) $(REST_PORT_SECURE) $(QDB_REST_BIN) $(REST_ARGS_SECURE) $(STRESS_FLAGS); \
	$(STRESS) drain --name secure --http http://127.0.0.1:$(REST_PORT_SECURE) --pid-file $(STRESS_PID_SECURE) --log $(STRESS_LOG_SECURE) \
	    --tool $(E2ETOOL) --out $(STRESS_OUT)/secure --rows $(STRESS_ROWS) --seed $(STRESS_SEED) \
	    --limit-rate $(STRESS_LIMIT_RATE) --exit-window $(STRESS_EXIT_WINDOW) \
	    --login-body $(REPO_ROOT)/user_private.key; \
	log_info "stress drain step: both clusters passed (seed=$(STRESS_SEED))"

# ------------------------------------------------------------------ cleanup

clean:
	@$(COMMON); stop_server $(REST_PID); stop_server $(REST_PID_SECURE); \
	rm -rf $(E2E_DIR)/actual $(E2E_DIR)/*.log $(E2E_DIR)/*.pid $(E2ETOOL) $(JDBC_DIR)/target
