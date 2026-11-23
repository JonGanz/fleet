# Build/verify fleet: one Go module (the repo root) with three binaries
# under cmd/ (fleet-task, fleet-run, fleet-cache) and shared packages under
# internal/.

BINARIES := fleet-task fleet-run fleet-cache
DIST     := dist
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS_BUILD := -trimpath -ldflags "-X main.version=$(VERSION)"

.DEFAULT_GOAL := help

.PHONY: build check clean fmt-check help install lint test uninstall vet

build: ## Build all binaries into dist/ (version-stamped)
	@mkdir -p $(DIST)
	@for b in $(BINARIES); do \
		echo "build $$b -> $(DIST)/$$b ($(VERSION))"; \
		go build $(GOFLAGS_BUILD) -o $(DIST)/$$b ./cmd/$$b || exit 1; \
	done

check: fmt-check vet test ## fmt-check + vet + test

clean: ## Remove dist/ and stray build artifacts
	rm -rf $(DIST)
	@for b in $(BINARIES); do rm -f $$b cmd/$$b/$$b; done

fmt-check: ## Fail if gofmt would change any file
	@out="$$(gofmt -l .)" || exit 1; \
	if [ -n "$$out" ]; then echo "$$out"; echo "gofmt: files above need formatting (run gofmt -w)" >&2; exit 1; fi

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "} {printf "  %-10s %s\n", $$1, $$2}'

install: ## Install binaries + skill via install.sh
	FLEET_VERSION="$(VERSION)" ./install.sh

lint: ## Run staticcheck (skipped if not on PATH)
	@if ! command -v staticcheck >/dev/null 2>&1; then \
		echo "lint: staticcheck not on PATH, skipping (go install honnef.co/go/tools/cmd/staticcheck@latest)"; \
		exit 0; \
	fi; \
	staticcheck ./...

test: ## go test -race
	go test -race ./...

uninstall: ## Remove what install put in place
	./install.sh --uninstall

vet: ## go vet
	go vet ./...
