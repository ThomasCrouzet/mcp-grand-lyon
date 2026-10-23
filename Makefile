# Development and release commands for grand-lyon-mcp.

SHELL := /bin/bash
.DEFAULT_GOAL := help

GO := go
GOVULNCHECK_VERSION := v1.9.0
BINARY := bin/grand-lyon-mcp
VERSION := $(shell sed -nE 's/^[[:space:]]*const Version[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' internal/version/version.go)
COVERAGE_PACKAGES := ./internal/...
COVERAGE_MINIMUM := 85.0
# Exclude CLI, MCP SDK, and live provider boundaries from the coverage gate.

.PHONY: help format format-check vet lint test vulnerability quality build run release clean

help: ## Show available commands.
	@awk 'BEGIN {FS = ":.*##"; printf "\ngrand-lyon-mcp commands\n\n"} \
		/^[a-zA-Z0-9_-]+:.*?##/ {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""

format: ## Format Go source files.
	$(GO) fmt ./...

format-check: ## Check Go source formatting.
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		printf 'Files require gofmt:\n%s\n' "$$files" >&2; \
		exit 1; \
	fi

vet: ## Run static checks.
	$(GO) mod verify
	$(GO) mod tidy -diff
	$(GO) vet ./...

lint: format-check vet ## Check formatting, modules, and Go diagnostics.

test: ## Run all tests with the race detector and retain local evidence.
	@set -euo pipefail; \
	if [ -n "$${GLMCP_ARTIFACT_DIR:-}" ]; then \
		artifact_dir="$$GLMCP_ARTIFACT_DIR"; \
		test ! -e "$$artifact_dir"; \
	else \
		run_root="$$(mktemp -d /tmp/grand-lyon-mcp-quality.XXXXXX)"; \
		artifact_dir="$$run_root/evidence"; \
	fi; \
	coverage_dir="$${GLMCP_COVERAGE_DIR:-$${artifact_dir}.coverage}"; \
	test ! -e "$$coverage_dir"; \
	mkdir -p "$$artifact_dir"; \
	GLMCP_ARTIFACT_DIR="$$artifact_dir" GLMCP_COVERAGE_DIR="$$coverage_dir" \
		$(GO) test -race -cover -covermode=atomic -coverpkg=$(COVERAGE_PACKAGES) \
		-coverprofile="$$artifact_dir/coverage-unit.out" -count=1 ./...; \
	$(GO) tool covdata textfmt -i="$$coverage_dir" \
		-o="$$artifact_dir/coverage-e2e.out"; \
	{ \
		echo 'mode: set'; \
		awk 'FNR == 1 {next} {key = $$1 " " $$2; if ($$3 > 0) covered[key] = 1; else if (!(key in covered)) covered[key] = 0} END {for (key in covered) print key, covered[key]}' \
			"$$artifact_dir/coverage-unit.out" "$$artifact_dir/coverage-e2e.out" \
			| LC_ALL=C sort; \
	} >"$$artifact_dir/coverage-all.out"; \
	awk 'NR == 1 || ($$1 !~ /\/internal\/adapter\/mcp\.go:/ && \
		$$1 !~ /\/internal\/cli\// && \
		$$1 !~ /\/internal\/providers\/live\.go:/ && \
		$$1 !~ /\/internal\/providers\/transitous\.go:/)' \
		"$$artifact_dir/coverage-all.out" >"$$artifact_dir/coverage.out"; \
	echo "Coverage for all internal packages:"; \
	$(GO) tool cover -func="$$artifact_dir/coverage-all.out" \
		| tee "$$artifact_dir/coverage-all.txt"; \
	echo "Coverage after equivalent legacy exclusions:"; \
	$(GO) tool cover -func="$$artifact_dir/coverage.out" | tee "$$artifact_dir/coverage.txt"; \
	coverage="$$(awk '/^total:/ {gsub(/%/, "", $$3); print $$3}' "$$artifact_dir/coverage.txt")"; \
	test -n "$$coverage"; \
	awk -v coverage="$$coverage" 'BEGIN { exit !(coverage >= $(COVERAGE_MINIMUM)) }'; \
	echo "Evidence: $$artifact_dir"; \
	echo "Coverage data: $$coverage_dir"

vulnerability: ## Check dependencies against the Go vulnerability database.
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

quality: ## Run the complete local quality gate.
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) vulnerability

build: ## Build the host binary.
	mkdir -p $(dir $(BINARY))
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BINARY) ./cmd/grand-lyon-mcp

run: ## Run the server from source.
	$(GO) run ./cmd/grand-lyon-mcp serve

release: ## Build release archives in RELEASE_DIR.
	@test -n "$(RELEASE_DIR)" || { echo "Set RELEASE_DIR to a new output path." >&2; exit 2; }
	scripts/build-release.sh "$(VERSION)" "$(RELEASE_DIR)"

clean: ## Remove local build output.
	@if [ -d bin ]; then find bin -depth -delete; fi
