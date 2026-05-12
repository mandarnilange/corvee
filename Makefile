# corvee — authoritative invocation surface.
#
# Per CLAUDE.md and the spec §12.7, all build/test/lint flows run through
# this Makefile. Do not invoke go test or golangci-lint ad-hoc; those bypass
# the gates CI relies on.

SHELL := /bin/bash

GO            ?= go
BIN_DIR       := bin
COVERAGE_FILE := coverage.out
GOLANGCI_VERSION := v2.5.0
PKG           := github.com/mandarnilange/corvee

DOMAIN_COV_MIN  := 90
USECASE_COV_MIN := 90

LDFLAGS := -X $(PKG)/internal/domain.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: help
help:
	@echo "Targets:"
	@echo "  build           - Build bin/corvee for the current platform"
	@echo "  build-all       - Cross-compile linux/{amd64,arm64} and darwin/arm64"
	@echo "  test            - Unit + adapter integration (no -race, no E2E). Fast loop."
	@echo "  test-unit       - Domain + usecase only. Sub-2s target."
	@echo "  test-race       - All tests with -race -count=10 (concurrency gate)"
	@echo "  test-e2e        - Build binary, run end-to-end tests"
	@echo "  test-coverage   - Coverage report; fails on domain<$(DOMAIN_COV_MIN)% or usecase<$(USECASE_COV_MIN)%"
	@echo "  lint            - gofmt -l, go vet, golangci-lint run"
	@echo "  ci              - lint && test-race && test-e2e && test-coverage && tasks-validate"
	@echo "  install-tools   - Install golangci-lint $(GOLANGCI_VERSION)"
	@echo "  hooks-install   - Symlink .githooks/* into .git/hooks/"
	@echo "  tasks-validate  - Validate .spec/tasks.json structure"
	@echo "  clean           - Remove $(BIN_DIR)/, $(COVERAGE_FILE), dist/"
	@echo "  release         - Build all platforms; emit dist/ with sha256 sums"

.PHONY: build
build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/corvee ./cmd/corvee

.PHONY: build-all
build-all:
	@mkdir -p $(BIN_DIR)
	GOOS=linux  GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/corvee-linux-amd64 ./cmd/corvee
	GOOS=linux  GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/corvee-linux-arm64 ./cmd/corvee
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/corvee-darwin-arm64 ./cmd/corvee

.PHONY: test
test:
	$(GO) test -short ./internal/... ./pkg/...

.PHONY: test-unit
test-unit:
	$(GO) test -short ./internal/domain/... ./internal/usecase/...

.PHONY: test-race
test-race:
	$(GO) test -race -count=10 ./internal/... ./pkg/... ./test/...

.PHONY: test-e2e
test-e2e: build
	$(GO) test ./test/e2e/...

.PHONY: test-coverage
test-coverage:
	@mkdir -p $(BIN_DIR)
	$(GO) test -coverprofile=$(COVERAGE_FILE) ./internal/domain/... ./internal/usecase/...
	@./scripts/check-coverage.sh $(COVERAGE_FILE) $(DOMAIN_COV_MIN) $(USECASE_COV_MIN)

GOLANGCI := $(shell command -v golangci-lint 2>/dev/null || echo $$($(GO) env GOPATH)/bin/golangci-lint)

.PHONY: lint
lint:
	@./scripts/check-gofmt.sh
	$(GO) vet ./...
	@test -x "$(GOLANGCI)" || { echo "golangci-lint not installed; run 'make install-tools'"; exit 1; }
	$(GOLANGCI) run

.PHONY: tasks-validate
tasks-validate:
	$(GO) run ./cmd/tasks-validate .spec/tasks.json

# dogfood validates that the project's own .tasks/ workspace is
# healthy. This is real eat-your-own-dog-food: the repo uses the
# binary it produces to track its own development. CI runs this on
# every push so the live workspace cannot rot.
#
# - `corvee validate` — schema, dependency graph, parent integrity
# - `corvee summary`  — surfaced as a structured smoke test
# - `corvee list`     — confirms the items/ tree is parseable
.PHONY: dogfood
dogfood: build
	@echo "==> Validating .tasks/ workspace via the binary"
	@./$(BIN_DIR)/corvee validate | tee /tmp/dogfood-validate.json | grep -q '"ok":true' \
		|| { echo "corvee validate reports issues:"; cat /tmp/dogfood-validate.json; exit 1; }
	@./$(BIN_DIR)/corvee summary > /tmp/dogfood-summary.json
	@echo "==> summary:"
	@./$(BIN_DIR)/corvee summary --pretty | head -20
	@echo "==> list (first 5 items):"
	@./$(BIN_DIR)/corvee list --limit 5 --pretty | head -40
	@echo "==> dogfood OK"

.PHONY: ci
ci: lint test-race test-e2e test-coverage tasks-validate

.PHONY: install-tools
install-tools:
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

.PHONY: hooks-install
hooks-install:
	@mkdir -p .git/hooks
	@for h in .githooks/*; do \
		name=$$(basename $$h); \
		ln -sf "../../$$h" ".git/hooks/$$name"; \
		chmod +x "$$h"; \
		echo "installed hook: $$name"; \
	done

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) $(COVERAGE_FILE) dist/

.PHONY: release
release: build-all
	@mkdir -p dist
	@cd $(BIN_DIR) && for f in corvee-*; do shasum -a 256 "$$f" > "../dist/$$f.sha256"; cp "$$f" "../dist/$$f"; done
	@echo "release artifacts in dist/"

# skill-tarball produces a publishable tarball of every skill folder
# under skills/ (SKILL.md + README.md + any references/). NO binaries —
# the binary is installed separately by the user via `go install` or a
# release artifact. Bundles both the day-to-day `corvee` skill and the
# `corvee-install` lifecycle skill (install / verify / upgrade /
# CLAUDE.md+AGENTS.md bootstrap).
.PHONY: skill-tarball
skill-tarball:
	@mkdir -p dist
	@tar -C skills -czf dist/corvee-skill.tar.gz corvee corvee-install
	@echo "wrote dist/corvee-skill.tar.gz (corvee + corvee-install)"
