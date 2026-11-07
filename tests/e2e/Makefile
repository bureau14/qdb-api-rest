# The entry point of the qdb-api-rest e2e harness (docs/e2e.md).
# It needs GNU make. Every path and port lives here and is passed down
# explicitly.
#
#   make e2etool                    build the Go tool of the v2 round trip into bin/
#   make jdbc                       build the JDBC smoke jar of the v2 round trip
#   make test-round-trip            the v2 round trip, one server per cluster
#   make test-stress                the stress, by hand: the drain step on both clusters
#   make old-server                 export master and build the old server into bin/, for the v1 comparison
#   make test-v1-compare            the v1 comparison, by hand: the old server next to the new one on both clusters
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

.PHONY: help services-check e2etool jdbc test-round-trip test-stress old-server test-v1-compare clean

help:
	@sed -n '2,12p' $(firstword $(MAKEFILE_LIST))

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
	    --insecure-cluster $(QDB_URI) --secure-cluster $(QDB_URI_SECURE) \
	    --secure-cluster-key $(REPO_ROOT)/cluster_public.key --secure-user-key $(REPO_ROOT)/user_private.key \
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
# STRESS_CLIENTS is three times the budget, not measured, so the budget
# step's clients queue for sessions.
STRESS_ROWS        ?= 200000
STRESS_SEED        ?= $(shell date +%s)
STRESS_LIMIT_RATE  ?= 500k
STRESS_GRACE       ?= 60s
STRESS_EXIT_WINDOW ?= 70
STRESS_CLIENTS     ?= 6
STRESS_FLAGS       := --listen-shutdown-grace $(STRESS_GRACE) --pool-max-sessions 2 --pool-per-user-max 2
STRESS_PID         := $(E2E_DIR)/stress.pid
STRESS_LOG         := $(E2E_DIR)/stress.log
STRESS_PID_SECURE  := $(E2E_DIR)/stress-secure.pid
STRESS_LOG_SECURE  := $(E2E_DIR)/stress-secure.log
STRESS_OUT         := $(E2E_DIR)/actual/stress
STRESS             := $(E2E_DIR)/stress.sh

# The stress always starts its servers, because the drain step ends the
# server with SIGTERM, so no URL form exists. The round trip's ports are
# reused, because the two suites never run at once on one machine, and a
# clash fails the bind loudly. Own pidfiles and logs, because the log is
# asserted. One cluster after the other: each server is started here,
# runs the budget step and then the drain step, which ends it and waits
# for the exit itself. The trap covers a failed step, where stop_server
# finds a dead pid or a server it may kill.
test-stress: services-check e2etool
	@$(COMMON); \
	check_service_running qdbd $(subst qdb://127.0.0.1:,,$(QDB_URI_SECURE)) \
	    || die "secure qdbd is not answering on $(QDB_URI_SECURE); run: bash scripts/tests/setup/start-services.sh"; \
	[[ -n "$(QDB_REST_BIN)" ]] || die "no server under test: set QDB_REST_BIN=<binary>"; \
	trap 'stop_server $(STRESS_PID); stop_server $(STRESS_PID_SECURE)' EXIT; \
	rm -rf $(STRESS_OUT); \
	start_server $(STRESS_PID) $(STRESS_LOG) $(REST_PORT) $(QDB_REST_BIN) $(REST_ARGS) $(STRESS_FLAGS); \
	$(STRESS) budget --name insecure --http http://127.0.0.1:$(REST_PORT) \
	    --tool $(E2ETOOL) --out $(STRESS_OUT)/insecure/budget --rows $(STRESS_ROWS) --seed $(STRESS_SEED) \
	    --clients $(STRESS_CLIENTS); \
	$(STRESS) drain --name insecure --http http://127.0.0.1:$(REST_PORT) --pid-file $(STRESS_PID) --log $(STRESS_LOG) \
	    --tool $(E2ETOOL) --out $(STRESS_OUT)/insecure/drain --rows $(STRESS_ROWS) --seed $(STRESS_SEED) \
	    --limit-rate $(STRESS_LIMIT_RATE) --exit-window $(STRESS_EXIT_WINDOW); \
	start_server $(STRESS_PID_SECURE) $(STRESS_LOG_SECURE) $(REST_PORT_SECURE) $(QDB_REST_BIN) $(REST_ARGS_SECURE) $(STRESS_FLAGS); \
	$(STRESS) budget --name secure --http http://127.0.0.1:$(REST_PORT_SECURE) \
	    --tool $(E2ETOOL) --out $(STRESS_OUT)/secure/budget --rows $(STRESS_ROWS) --seed $(STRESS_SEED) \
	    --clients $(STRESS_CLIENTS) --login-body $(REPO_ROOT)/user_private.key; \
	$(STRESS) drain --name secure --http http://127.0.0.1:$(REST_PORT_SECURE) --pid-file $(STRESS_PID_SECURE) --log $(STRESS_LOG_SECURE) \
	    --tool $(E2ETOOL) --out $(STRESS_OUT)/secure/drain --rows $(STRESS_ROWS) --seed $(STRESS_SEED) \
	    --limit-rate $(STRESS_LIMIT_RATE) --exit-window $(STRESS_EXIT_WINDOW) \
	    --login-body $(REPO_ROOT)/user_private.key; \
	log_info "stress budget and drain steps: both clusters passed (seed=$(STRESS_SEED))"

# ---------------------------------------------------------- v1 comparison

# The old server (docs/e2e.md, The v1 comparison): master, exported into
# OLD_SRC and built against this repository's qdb/, by hand, for the
# comparison. OLD_SRC is gitignored. The binary lands next to qdb_rest,
# because every cgo binary the harness runs sits in bin/.
OLD_SRC := $(E2E_DIR)/old-server
OLD_BIN := $(BIN_DIR)/qdb_rest_old$(EXE_EXT)

# git archive leaves .git untouched, where a worktree would register
# itself and need pruning. The export's own .envrc resolves qdb/ next to
# itself, so the repository's qdb/ is symlinked in, and the build sources
# that .envrc and links from the export's own vendor/.
old-server:
	@$(COMMON); rm -rf $(OLD_SRC); mkdir -p $(OLD_SRC) $(BIN_DIR); \
	cd $(REPO_ROOT) && git archive master | tar -x -C $(OLD_SRC); \
	ln -s $(QDB_DIR) $(OLD_SRC)/qdb; \
	cd $(OLD_SRC) && source ./.envrc && \
	    go build -mod=vendor -buildvcs=false -o $(OLD_BIN) ./apps/qdb_rest && \
	    log_info "built $(OLD_BIN)"

# The comparison runs the old server next to the new one on each
# cluster. The new servers reuse REST_ARGS and REST_ARGS_SECURE with the
# round trip's pidfiles and logs, so the comparison and the round trip
# test one binary the same way.

# The old server's ports. It takes an http port and a tls port per
# cluster, because a secure old server must bind tls, and it then
# redirects every plain request there, so the driver dials the secure
# one at its tls port.
OLD_PORT            ?= 40096
OLD_TLS_PORT        ?= 40097
OLD_PORT_SECURE     ?= 40098
OLD_TLS_PORT_SECURE ?= 40099

# The old server's pidfiles and logs, next to the round trip's.
OLD_PID        := $(E2E_DIR)/old.pid
OLD_LOG        := $(E2E_DIR)/old.log
OLD_PID_SECURE := $(E2E_DIR)/old-secure.pid
OLD_LOG_SECURE := $(E2E_DIR)/old-secure.log

# The run's working files: the driver's bodies per cluster, and the old
# server's configs and tls pair, written per run.
COMPARE_OUT     := $(E2E_DIR)/actual/compare
OLD_CONF        := $(COMPARE_OUT)/old.json
OLD_CONF_SECURE := $(COMPARE_OUT)/old-secure.json
OLD_TLS_CERT    := $(COMPARE_OUT)/old.cert.pem
OLD_TLS_KEY     := $(COMPARE_OUT)/old.key.pem

# The driver and its table. COMPARE_TABLE is the round trip's first
# table, which make test-round-trip has seeded on both clusters, and
# the target does not run it.
COMPARE_TABLE ?= e2e_csv
COMPARE       := $(E2E_DIR)/compare-v1.sh

# The driver starts both servers of each cluster itself, runs the
# comparison and stops them, and the values it uses are exported here,
# because a recipe carries no comment between its lines and the
# driver's steps need them. Both clusters must answer, and the secure
# probe lives here as in the round trip.
export OLD_BIN OLD_PORT OLD_TLS_PORT OLD_PORT_SECURE OLD_TLS_PORT_SECURE
export OLD_PID OLD_LOG OLD_PID_SECURE OLD_LOG_SECURE
export COMPARE_OUT OLD_CONF OLD_CONF_SECURE OLD_TLS_CERT OLD_TLS_KEY COMPARE_TABLE
export QDB_REST_BIN REST_PORT REST_ARGS REST_PID REST_LOG
export REST_PORT_SECURE REST_ARGS_SECURE REST_PID_SECURE REST_LOG_SECURE QDB_URI_SECURE
test-v1-compare: services-check old-server
	@$(COMMON); \
	check_service_running qdbd $(subst qdb://127.0.0.1:,,$(QDB_URI_SECURE)) \
	    || die "secure qdbd is not answering on $(QDB_URI_SECURE); run: bash scripts/tests/setup/start-services.sh"; \
	[[ -n "$(QDB_REST_BIN)" ]] || die "no server under test: set QDB_REST_BIN=<binary>"; \
	$(COMPARE)

# ------------------------------------------------------------------ cleanup

clean:
	@$(COMMON); stop_server $(REST_PID); stop_server $(REST_PID_SECURE); stop_server $(OLD_PID); stop_server $(OLD_PID_SECURE); \
	rm -rf $(E2E_DIR)/actual $(E2E_DIR)/*.log $(E2E_DIR)/*.pid $(E2ETOOL) $(JDBC_DIR)/target $(OLD_SRC) $(OLD_BIN)
