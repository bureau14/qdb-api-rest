# qdb-api-rest -- build entry point. Test targets live in tests/e2e/Makefile.

.PHONY: build

build:
	go build -o bin/qdb_rest ./cmd/qdb_rest
