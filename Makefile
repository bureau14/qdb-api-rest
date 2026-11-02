# qdb-api-rest -- build entry point. Test targets live in tests/e2e/Makefile.

# Recipes that compile Go source the .envrc first (the CGO environment;
# needs bash for ${BASH_SOURCE[0]}).
SHELL := bash

GOLANGCI_LINT := bin/golangci-lint

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

.PHONY: build lint golangci-lint

build:
	. ./.envrc && GOAMD64=$(GOAMD64) go build -trimpath -mod=vendor -ldflags "$(LDFLAGS)" -o bin/qdb_rest ./cmd/qdb_rest

# The linter follows its latest release, installed by this target on
# every run, so no builder image holds a version. A Go release can change
# the export data format the linter's typecheck reads, and only a newer
# golangci-lint reads the new format, so a pinned linter breaks when the
# toolchain moves (owner, 2026-11-02). go install caches the build, so a
# run after the first costs one version lookup.
golangci-lint:
	GOBIN=$(abspath bin) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

lint: golangci-lint
	. ./.envrc && $(GOLANGCI_LINT) run ./...
