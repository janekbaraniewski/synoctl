SHELL          := /bin/bash
BIN_DIR        := bin
BIN            := $(BIN_DIR)/synoctl
PKG            := github.com/janbaraniewski/synology-ctl
MAIN           := ./cmd/synoctl
GO             ?= go
GOFLAGS        ?=
VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT         := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE           := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS        := -s -w \
                  -X $(PKG)/internal/cli.version=$(VERSION) \
                  -X $(PKG)/internal/cli.commit=$(COMMIT) \
                  -X $(PKG)/internal/cli.date=$(DATE)

GOFILES        := $(shell find . -name '*.go' -not -path './vendor/*' -not -path './bin/*' -not -path './dist/*')

GORELEASER     ?= goreleaser
ACTIONLINT     ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

.PHONY: help build build-all run install tidy fmt fmt-check vet lint test discover login \
        workflow-lint release-check release-snapshot clean ci all

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nsynoctl — make targets\n\n"} \
	  /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } \
	  /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build

build: $(BIN) ## Compile a debug binary to ./bin/synoctl

$(BIN): $(GOFILES) go.mod go.sum | $(BIN_DIR)
	@echo "» building $(BIN) ($(VERSION) / $(COMMIT))"
	@CGO_ENABLED=0 $(GO) build -trimpath $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) $(MAIN)

build-all: ## Cross-compile binaries for darwin/linux × amd64/arm64
	@mkdir -p $(BIN_DIR)
	@for os in linux darwin; do \
	  for arch in amd64 arm64; do \
	    echo "» building $(BIN_DIR)/synoctl-$$os-$$arch"; \
	    CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" \
	      -o $(BIN_DIR)/synoctl-$$os-$$arch $(MAIN); \
	  done; \
	done

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

install: ## Install to $GOPATH/bin (or $HOME/go/bin)
	@echo "» installing synoctl ($(VERSION))"
	@CGO_ENABLED=0 $(GO) install -trimpath -ldflags "$(LDFLAGS)" $(MAIN)

##@ Run

run: build ## Launch the TUI against the configured (or auto-discovered) NAS
	@$(BIN)

discover: build ## Scan the local network for Synology devices
	@$(BIN) discover

login: build ## Configure credentials for a NAS profile
	@$(BIN) login

##@ Quality

tidy: ## go mod tidy
	@$(GO) mod tidy

fmt: ## Format all Go files
	@gofmt -w $(GOFILES)

fmt-check: ## Check formatting (CI-friendly: non-zero on any unformatted file)
	@test -z "$$(gofmt -l $(GOFILES))" || \
	  (echo "go files need formatting:" && gofmt -l $(GOFILES) && exit 1)

vet: ## go vet ./...
	@$(GO) vet ./...

lint: fmt-check vet ## Run all linters

workflow-lint: ## Lint .github/workflows with actionlint
	@$(ACTIONLINT) .github/workflows/*.yml .github/workflows/*.yaml

test: ## Run unit tests
	@$(GO) test ./... -race -count=1

##@ Release

release-check: ## Validate .goreleaser.yml
	@$(GORELEASER) check

release-snapshot: ## Build a local snapshot release (no publish)
	@$(GORELEASER) release --snapshot --clean --skip=publish

##@ Housekeeping

clean: ## Remove build artefacts
	@rm -rf $(BIN_DIR) dist/

ci: lint test workflow-lint build-all release-check ## Run the full local CI suite

all: tidy fmt vet test build ## tidy → fmt → vet → test → build

.DEFAULT_GOAL := help
