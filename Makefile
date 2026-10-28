# DevBox build entry point. Every build, test, lint and generate step runs through this file.

SHELL := bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

# ---------------------------------------------------------------------------
# Build metadata (injected into cmd/devbox via -ldflags)
# ---------------------------------------------------------------------------
# Semantic version from the newest release tag (vX.Y.Z, or devbox-vX.Y.Z from before
# 0.1.1), without prefix; commits after the tag and local changes go into build
# metadata (0.1.0+3.gabc1234.dirty), so requires.devbox ranges still match.
VERSION ?= $(or $(shell git describe --tags --match 'v[0-9]*' --match 'devbox-v[0-9]*' --dirty 2>/dev/null | sed -e 's/^devbox-//' -e 's/^v//' -e 's/-\([0-9][0-9]*\)-g\([0-9a-f]*\)/+\1.g\2/' -e 's/-dirty$$/.dirty/' -e 's/^\([0-9.]*\)\.dirty$$/\1+dirty/'),0.0.0-dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell TZ=UTC git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

BIN_DIR := bin
BINARY  := $(BIN_DIR)/devbox

# ---------------------------------------------------------------------------
# Tools (Go tools are pinned via the tool directives in go.mod)
# ---------------------------------------------------------------------------
GO            := go
GO_JSONSCHEMA := $(GO) tool go-jsonschema
OAPI_CODEGEN  := $(GO) tool oapi-codegen
GOLANGCI_LINT := $(GO) tool golangci-lint
GOFUMPT       := $(GO) tool gofumpt
NPX           := npx --no --

# ---------------------------------------------------------------------------
# Code generation
# ---------------------------------------------------------------------------
SCHEMA_DIR  := schema
SCHEMAS     := $(sort $(wildcard $(SCHEMA_DIR)/*.v1.json))
OPENAPI     := api/openapi.json
GO_GEN_DIR  := internal/gen

# Go packages of this module. npm packages can ship Go files (node_modules is not
# ignored by ./...), so they are filtered out.
GO_PACKAGES = $(shell $(GO) list ./... | grep -v '/node_modules/')
GO_PKG_DIRS = $(shell $(GO) list -f '{{.Dir}}' ./... | grep -v '/node_modules/' | sed "s|^$(CURDIR)/|./|")
GO_GEN_FILE := types.go
GEN_DIRS    := $(GO_GEN_DIR) web/src/app/gen sdk/ts/src/gen

# Flags for go-jsonschema; each schema becomes its own package to avoid type name clashes.
GO_JSONSCHEMA_FLAGS := --tags json

# Plugin tests (bats); empty until plugins with tests exist.
BATS_DIRS := $(wildcard plugins/*/tests)

.PHONY: help
help: ## List all targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9_-]+:.*## / { printf "  %-16s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: tools
tools: ## Install npm dependencies (Go tools come from go.mod)
	npm ci

.PHONY: generate
generate: ## Generate Go and TypeScript types from schema/ and api/openapi.json
	@# Go types: one package per schema, e.g. schema/history-entry.v1.json -> internal/gen/historyentry
	@mkdir -p $(GO_GEN_DIR)
	@find $(GO_GEN_DIR) -name '$(GO_GEN_FILE)' -delete
	@for schema in $(SCHEMAS); do \
		name=$$(basename "$$schema" .v1.json); \
		pkg=$${name//-/}; \
		out=$(GO_GEN_DIR)/$$pkg/$(GO_GEN_FILE); \
		echo "go-jsonschema  $$schema -> $$out"; \
		mkdir -p "$$(dirname "$$out")"; \
		$(GO_JSONSCHEMA) $(GO_JSONSCHEMA_FLAGS) -p "$$pkg" -o "$$out" "$$schema"; \
	done
	@# Go API types from the OpenAPI contract
	@if [ -f $(OPENAPI) ]; then \
		echo "oapi-codegen   $(OPENAPI) -> $(GO_GEN_DIR)/api/$(GO_GEN_FILE)"; \
		mkdir -p $(GO_GEN_DIR)/api; \
		$(OAPI_CODEGEN) -generate types -package api $(OPENAPI) > $(GO_GEN_DIR)/api/$(GO_GEN_FILE); \
	fi
	@# TypeScript types (web and sdk/ts each define a generate script)
	npm run generate --workspaces --if-present
	@# Format generated Go code
	@if [ -n "$$(find $(GO_GEN_DIR) -name '*.go' -print -quit)" ]; then $(GOFUMPT) -w $(GO_GEN_DIR); fi

.PHONY: generate-check
generate-check: generate ## Fail if generated code differs from the committed state
	@if ! git diff --exit-code -- $(GEN_DIRS) || [ -n "$$(git ls-files -o --exclude-standard -- $(GEN_DIRS))" ]; then \
		echo >&2; \
		echo "Generated code is out of date. Run 'make generate' and commit the result." >&2; \
		git ls-files -o --exclude-standard -- $(GEN_DIRS) >&2; \
		exit 1; \
	fi

.PHONY: build
build: ## Build the workspaces and the devbox binary into bin/
	npm run build --workspaces --if-present
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/devbox

.PHONY: dev
dev: ## Run the core and the Angular dev server (UI on http://localhost:4200)
	@# The dev server proxies /api to the core and its Origin is allowed explicitly.
	trap 'kill 0' EXIT; \
	$(GO) run ./cmd/devbox serve --allow-origin http://localhost:4200 & \
	npm --workspace web run start & \
	wait

.PHONY: dev-mock
dev-mock: ## Run the UI against typed mock data, without the core (http://localhost:4200)
	npm --workspace web run start:mock

.PHONY: test
test: ## Run Go tests (race detector), CLI flows (testscript), workspace tests and plugin bats tests
	@# The race detector needs cgo; release builds stay CGO_ENABLED=0.
	CGO_ENABLED=1 $(GO) test -race $(GO_PACKAGES)
	$(GO) test -run TestScripts ./cmd/devbox
	npm test --workspaces --if-present
ifneq ($(strip $(BATS_DIRS)),)
	CGO_ENABLED=0 $(GO) build -o $(BINARY) ./cmd/devbox
	PATH="$(CURDIR)/$(BIN_DIR):$$PATH" bats $(BATS_DIRS)
endif

.PHONY: lint
lint: ## Run golangci-lint, shellcheck, ESLint, Prettier and the JSON/literal checks
	$(GOLANGCI_LINT) run $(GO_PKG_DIRS)
	@files=$$(git ls-files -co --exclude-standard -- '*.sh' '*.bash' | while IFS= read -r f; do [ -f "$$f" ] && echo "$$f"; done || true); \
	if [ -n "$$files" ]; then echo "shellcheck"; echo "$$files" | xargs shellcheck -x --source-path=SCRIPTDIR; fi
	$(NPX) eslint .
	npm run --silent format:check
	$(GO) run ./internal/tools/jsoncheck
	$(GO) run ./internal/tools/literalcheck
	CGO_ENABLED=0 $(GO) build -o $(BINARY) ./cmd/devbox
	@for dir in plugins/*/; do echo "plugin validate $$dir"; $(BINARY) plugin validate "$$dir"; done

.PHONY: e2e
e2e: ## End-to-end tests (Playwright, arrives with AP-14)
	@echo "No end-to-end tests yet: Playwright tests arrive with AP-14."

.PHONY: snapshot
snapshot: ## Build a local GoReleaser snapshot into dist/
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser not found on PATH; install GoReleaser v2 first." >&2; exit 1; }
	goreleaser release --snapshot --clean
