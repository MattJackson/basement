# basement Makefile — local quality gates orchestration.
#
# Mirrors the .github/workflows/quality.yml CI jobs so a developer
# can run `make quality` before pushing and reproduce the same
# pass/fail signal locally. Cycle v1.11.0.12.

.DEFAULT_GOAL := help

GO        ?= go
PNPM      ?= pnpm
FRONTEND  ?= frontend

.PHONY: help
help:
	@echo "basement quality targets:"
	@echo "  make quality   — run lint + vet + test + sec + vulns (Go side)"
	@echo "  make lint      — golangci-lint run ./..."
	@echo "  make vet       — go vet ./..."
	@echo "  make test      — go test -race ./..."
	@echo "  make sec       — gosec -conf .gosec.yml ./..."
	@echo "  make vulns     — govulncheck ./..."
	@echo "  make frontend  — pnpm lint + tsc --noEmit + test:run"
	@echo "  make smoke     — pnpm smoke (curated against deploy)"
	@echo "  make smoke-full— pnpm smoke:full (comprehensive walk)"
	@echo "  make build     — go build ./... + pnpm build"

.PHONY: quality
quality: lint vet test sec vulns

.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed — see CONTRIBUTING.md"; exit 1; }
	golangci-lint run ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: test
test:
	$(GO) test -race ./...

.PHONY: sec
sec:
	@command -v gosec >/dev/null 2>&1 || { echo "gosec not installed — go install github.com/securego/gosec/v2/cmd/gosec@latest"; exit 1; }
	gosec -conf .gosec.yml ./...

.PHONY: vulns
vulns:
	@command -v govulncheck >/dev/null 2>&1 || { echo "govulncheck not installed — go install golang.org/x/vuln/cmd/govulncheck@latest"; exit 1; }
	govulncheck ./...

.PHONY: frontend
frontend:
	$(PNPM) -C $(FRONTEND) lint
	$(PNPM) -C $(FRONTEND) exec tsc --noEmit
	$(PNPM) -C $(FRONTEND) test:run

.PHONY: build
build:
	$(GO) build ./...
	$(PNPM) -C $(FRONTEND) build

.PHONY: smoke
smoke:
	$(PNPM) -C $(FRONTEND) smoke

.PHONY: smoke-full
smoke-full:
	$(PNPM) -C $(FRONTEND) smoke:full

.PHONY: fuzz-audit
fuzz-audit:
	$(GO) test -fuzz=FuzzMatchFilter -fuzztime=30s ./internal/audit/...

.PHONY: precommit-install
precommit-install:
	@command -v pre-commit >/dev/null 2>&1 || { echo "pre-commit not installed — pip install pre-commit"; exit 1; }
	pre-commit install
