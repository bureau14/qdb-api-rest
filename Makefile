# qdb-api-rest -- build entry point. Test targets live in tests/e2e/Makefile.

GOLANGCI_LINT_VERSION := v2.13.1
GOLANGCI_LINT         := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

# Build metadata injected via -ldflags (qdb-nats-connector ADR-011); the
# VERSION file is the single version-string location in this repo.
# scripts/cicd/20.build.sh composes the same flags for CI, where GNU make
# is not available on every platform.
VERSION    := $(shell cat VERSION)
GIT_SHA    := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
BUILD_MODE ?= release
GOAMD64    ?=

LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(GIT_SHA) \
           -X main.buildTime=$(BUILD_TIME) \
           -X main.buildMode=$(BUILD_MODE) \
           -X main.goamd64=$(GOAMD64)

.PHONY: build lint

build:
	GOAMD64=$(GOAMD64) go build -trimpath -mod=vendor -ldflags "$(LDFLAGS)" -o bin/qdb_rest ./cmd/qdb_rest

# The linter is pinned here and installed by this target, so the pin
# lives in the repository rather than in a builder image.
$(GOLANGCI_LINT):
	GOBIN=$(abspath bin) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...
