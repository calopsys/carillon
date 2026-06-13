BINARY      := carillon
PKG         := github.com/calopsys/carillon
MAIN        := ./cmd/carillon
BIN_DIR     := bin
CONFIG      := docs/config.example.toml

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X $(PKG)/internal/cli.buildVersion=$(VERSION)

GO          ?= go
GOFLAGS     ?=

.DEFAULT_GOAL := build

## help: list available targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'

## build: compile the binary into bin/
.PHONY: build
build:
	mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(MAIN)

## run: build then run against the example config (ARGS="check traefik --dry-run")
.PHONY: run
run: build
	$(BIN_DIR)/$(BINARY) -c $(CONFIG) $(ARGS)

## fmt: format all Go sources
.PHONY: fmt
fmt:
	$(GO) fmt ./...

## fmt-check: fail if any file is not gofmt-clean
.PHONY: fmt-check
fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "not gofmt-clean:"; echo "$$out"; exit 1; fi

## vet: run go vet
.PHONY: vet
vet:
	$(GO) vet ./...

## lint: run golangci-lint
.PHONY: lint
lint:
	golangci-lint run ./...

## test: run unit tests with race detector
.PHONY: test
test:
	$(GO) test -race ./...

## cover: run tests and write a coverage profile + HTML report
.PHONY: cover
cover:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html"

## tidy: sync go.mod/go.sum
.PHONY: tidy
tidy:
	$(GO) mod tidy

## verify: check module integrity and that go.mod/go.sum are tidy (no writes)
.PHONY: verify
verify:
	$(GO) mod verify
	$(GO) mod tidy -diff

## validate: build and validate the example config offline
.PHONY: validate
validate: build
	$(BIN_DIR)/$(BINARY) validate -c $(CONFIG)

## check: fmt-check + vet + lint + test (the pre-commit gate)
.PHONY: check
check: fmt-check vet lint test

## ci: full gate including a clean build
.PHONY: ci
ci: verify check build

## clean: remove build and coverage artifacts
.PHONY: clean
clean:
	rm -rf $(BIN_DIR) coverage.out coverage.html
