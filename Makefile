# Build/verify the three independent fleet modules. Each module is its own Go
# module (no go.work, by design), so every target loops over them and runs Go
# from inside each module directory.

MODULES := fleet-task fleet-run fleet-cache
DIST    := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS_BUILD := -trimpath -ldflags "-X main.version=$(VERSION)"

.DEFAULT_GOAL := help

.PHONY: build check clean fmt-check help install lint test uninstall vet

build: ## Build all binaries into dist/ (version-stamped)
	@mkdir -p $(DIST)
	@for m in $(MODULES); do \
		echo "build $$m -> $(DIST)/$$m ($(VERSION))"; \
		(cd $$m && go build $(GOFLAGS_BUILD) -o ../$(DIST)/$$m .) || exit 1; \
	done

check: fmt-check vet test ## fmt-check + vet + test

clean: ## Remove dist/ and stray in-module build artifacts
	rm -rf $(DIST)
	@for m in $(MODULES); do rm -f $$m/$$m; done

fmt-check: ## Fail if gofmt would change any file
	@out=""; \
	for m in $(MODULES); do \
		files=$$(gofmt -l $$m) || exit 1; \
		out="$$out$$files"; \
		[ -z "$$files" ] || echo "$$files"; \
	done; \
	if [ -n "$$out" ]; then echo "gofmt: files above need formatting (run gofmt -w)" >&2; exit 1; fi

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "} {printf "  %-10s %s\n", $$1, $$2}'

install: ## Install binaries + skill via install.sh
	FLEET_VERSION="$(VERSION)" ./install.sh

lint: ## Run staticcheck per module (skipped if not on PATH)
	@if ! command -v staticcheck >/dev/null 2>&1; then \
		echo "lint: staticcheck not on PATH, skipping (go install honnef.co/go/tools/cmd/staticcheck@latest)"; \
		exit 0; \
	fi; \
	for m in $(MODULES); do \
		echo "staticcheck $$m"; \
		(cd $$m && staticcheck ./...) || exit 1; \
	done

test: ## go test -race in every module
	@for m in $(MODULES); do \
		echo "test $$m"; \
		(cd $$m && go test -race ./...) || exit 1; \
	done

uninstall: ## Remove what install put in place
	./install.sh --uninstall

vet: ## go vet in every module
	@for m in $(MODULES); do \
		echo "vet $$m"; \
		(cd $$m && go vet ./...) || exit 1; \
	done
