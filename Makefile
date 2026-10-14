# qdb-api-rest -- build entry point. Test targets live in tests/e2e/Makefile.

# Recipes that compile Go source the .envrc first (the CGO environment;
# needs bash for ${BASH_SOURCE[0]}).
SHELL := bash

GOLANGCI_LINT_VERSION := v2.13.1
GOLANGCI_LINT         := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

# Build metadata injected via -ldflags; composition rule:
# scripts/cicd/AGENTS.md. The VERSION file is the single version-string
# location (docs/brief.md, "Versioning and release").
VERSION    := $(shell cat VERSION)
GIT_SHA    := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
BUILD_MODE ?= release
GOAMD64    ?=

BUILD_PKG  := github.com/bureau14/qdb-api-rest/internal/build

LDFLAGS := -X $(BUILD_PKG).Version=$(VERSION) \
           -X $(BUILD_PKG).Commit=$(GIT_SHA) \
           -X $(BUILD_PKG).BuildTime=$(BUILD_TIME) \
           -X $(BUILD_PKG).BuildMode=$(BUILD_MODE) \
           -X $(BUILD_PKG).GOAMD64=$(GOAMD64)

.PHONY: build lint

build:
	. ./.envrc && GOAMD64=$(GOAMD64) go build -trimpath -mod=vendor -ldflags "$(LDFLAGS)" -o bin/qdb_rest ./cmd/qdb_rest

# The linter is pinned here and installed by this target, so the pin
# lives in the repository rather than in a builder image.
$(GOLANGCI_LINT):
	GOBIN=$(abspath bin) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@

lint: $(GOLANGCI_LINT)
	. ./.envrc && $(GOLANGCI_LINT) run ./...
