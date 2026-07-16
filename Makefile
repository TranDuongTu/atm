# Makefile - atm build/test/lint targets. Build artifacts go to gitignored bin/.
GO ?= go
BIN := bin
BINARY := $(BIN)/atm
PKG := ./... ./libs/eventsource/...

.PHONY: all build test lint vet fmt clean install help dogfood \
        scripts-test release release-upload release-smoke install-smoke \
        version-bump install-release install-dist dist

all: build

GO_LDFLAGS :=
ifneq ($(wildcard .git/),)
  GO_LDFLAGS := -X 'atm/internal/version.Version=$(shell git describe --tags --dirty --always)' \
                -X 'atm/internal/version.Commit=$(shell git rev-parse --short HEAD)' \
                -X 'atm/internal/version.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)'
endif

## build: compile the atm binary into bin/ with ldflags-injected version
build:
	@mkdir -p $(BIN)
	$(GO) build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BINARY) ./cmd/atm

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

# install: build atm via go install (resolves GOBIN/GOPATH automatically;
# avoids the old mkdir+cp approach that failed when $GOBIN was unset)
install:
	$(GO) install -ldflags "$(GO_LDFLAGS)" ./cmd/atm

## help: list targets
help:
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## verify: the AGENTS.md verify step - build + test + scripts-test.
verify:
	$(MAKE) build
	$(MAKE) test
	$(MAKE) scripts-test

## scripts-test: POSIX sh unit tests for scripts/_release_lib.sh.
scripts-test:
	tests/scripts/runner.sh

## dogfood: bootstrap the ATM project + follow-on tasks in the machine-global store (idempotent, opt-in)
dogfood: build
	./scripts/dogfood.sh $(BINARY)

## release: cut a release. Required: VERSION=vX.Y.Z. Optional: DRY_RUN=1, --no-edit, --from-ci.
release:
	@test -n "$(VERSION)" || { echo "VERSION=vX.Y.Z required"; exit 2; }
	scripts/release.sh VERSION=$(VERSION) $(RELEASE_ARGS)

## release-upload: retry the GitLab upload phase for an existing tag.
release-upload:
	@test -n "$(VERSION)" || { echo "VERSION=vX.Y.Z required"; exit 2; }
	scripts/release.sh VERSION=$(VERSION) --phase=8

## release-smoke: end-to-end DRY_RUN=1 release through dist/.
release-smoke:
	scripts/release.sh VERSION=v0.0.0-smoke DRY_RUN=1 --no-edit --no-preflight-tag

## install-smoke: install.sh against a local http server serving dist/.
install-smoke:
	tests/scripts/install-smoke.sh

## version-bump: regenerate internal/version/version.go from git state (no commit).
##   Pin REL_DATE/REL_COMMIT for deterministic output (tests/reproducible builds).
version-bump:
	scripts/release.sh VERSION=dev --phase=2 --no-preflight-tag

## install-release: convenience wrapper around scripts/install.sh.
install-release:
	FORGE=$(or $(FORGE),gitlab) REPO=$(or $(REPO),$(DEFAULT_REPO)) VERSION=$(VERSION) PREFIX=$(PREFIX) scripts/install.sh

## install-dist: install from a local dist/ directory (no network). VERSION defaults to dist/LATEST.
install-dist:
	FORGE=dist VERSION=$(VERSION) PREFIX=$(PREFIX) scripts/install.sh

## dist: alias for release-smoke (produces dist/ without committing).
dist: release-smoke