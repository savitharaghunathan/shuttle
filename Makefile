# shuttle — local AI software factory
#
#   make help              show targets
#   make build             compile bin/shuttle
#   make test-unit         fast unit tests (no integration tag)
#   make test-integration  CLI pipeline integration tests
#   make test              unit + integration
#   make verify            build + test
#   make serve-test-app    serve API from SHUTTLE_TEST_APP (~/local_dev/test)
#   make smoke-test-app    API recovery smoke (server must be running)

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

MODULE   := github.com/savitharaghunathan/shuttle
CMD      := ./cmd/shuttle
BIN      := bin/shuttle
GO       ?= go
GOFLAGS  ?=
TEST_PKGS := ./...
SHUTTLE_TEST_APP ?= $(HOME)/local_dev/test
TEST_APP_SCRIPTS := examples/scripts

.PHONY: help build install run clean \
        test test-unit test-integration test-race verify \
        serve-test-app smoke-test-app

.DEFAULT_GOAL := help

help: ## show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make [target]\n\nTargets:\n"} \
		/^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## build bin/shuttle
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o $(BIN) $(CMD)
	@echo "built $(BIN)"

install: build ## install shuttle to $$GOBIN or $$GOPATH/bin
	$(GO) install $(GOFLAGS) $(CMD)

run: build ## run shuttle (pass ARGS="workers", etc.)
	./$(BIN) $(ARGS)

clean: ## remove build artifacts
	rm -rf bin/
	$(GO) clean -testcache

test-unit: ## unit tests only (excludes //go:build integration)
	$(GO) test $(GOFLAGS) -count=1 $(TEST_PKGS)

test-integration: ## integration tests (CLI pipelines; requires repo checkout)
	$(GO) test $(GOFLAGS) -count=1 -tags=integration ./internal/cli/...

test: test-unit test-integration ## unit + integration tests

test-race: ## unit tests with -race
	$(GO) test $(GOFLAGS) -race -count=1 $(TEST_PKGS)

verify: build test ## build and run all tests

serve-test-app: build ## serve API from SHUTTLE_TEST_APP checkout (port 8080)
	SHUTTLE_TEST_APP="$(SHUTTLE_TEST_APP)" SHUTTLE_BIN="$(CURDIR)/$(BIN)" \
		$(TEST_APP_SCRIPTS)/serve-test-app.sh

smoke-test-app: build ## API recovery smoke; run serve-test-app in another terminal first
	SHUTTLE_TEST_APP="$(SHUTTLE_TEST_APP)" SHUTTLE_BIN="$(CURDIR)/$(BIN)" \
		ISSUE_URL="$(ISSUE_URL)" TEMPLATE="$(TEMPLATE)" AGENT="$(AGENT)" \
		$(TEST_APP_SCRIPTS)/smoke-test-app.sh
