# qdb-api-rest -- build entry point. Test targets live in tests/e2e/Makefile.

GOLANGCI_LINT_VERSION := v2.13.1
GOLANGCI_LINT         := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

.PHONY: build lint

build:
	go build -o bin/qdb_rest ./cmd/qdb_rest

# The linter is pinned here and installed by this target, so the pin
# lives in the repository rather than in a builder image.
$(GOLANGCI_LINT):
	GOBIN=$(abspath bin) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...
