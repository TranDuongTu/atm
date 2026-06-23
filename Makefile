# Makefile - atm build/test/lint targets. Build artifacts go to gitignored bin/.
GO ?= go
BIN := bin
BINARY := $(BIN)/atm
PKG := ./...

.PHONY: all build test lint vet fmt clean install help

all: build

## build: compile the atm binary into bin/
build:
	@mkdir -p $(BIN)
	$(GO) build -o $(BINARY) ./cmd/atm

## test: run all tests
test:
	$(GO) test $(PKG)

## test-race: run tests with the race detector
test-race:
	$(GO) test -race $(PKG)

## vet: go vet
vet:
	$(GO) vet $(PKG)

## fmt: gofmt (report only)
fmt:
	@$(GO) fmt $(PKG)

## fmt-check: fail if any file is not formatted
fmt-check:
	@out=$$($(GO) fmt $(PKG)); if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

## lint: vet + fmt-check (golangci-lint if installed)
lint: vet fmt-check
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipping"

## build-tui: smoke-build the TUI path (alias of build; kept for discoverability)
build-tui: build

## clean: remove build artifacts
clean:
	rm -rf $(BIN)

## install: build and copy the binary to $$GOBIN (or $$GOPATH/bin)
install: build
	@mkdir -p $$GOBIN
	cp $(BINARY) $$GOBIN/

## help: list targets
help:
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## verify: the AGENTS.md verify step - build + test
verify:
	$(MAKE) build
	$(MAKE) test