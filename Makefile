# basement Makefile — local quality gates orchestration.
#
# Mirrors the .github/workflows/quality.yml CI jobs so a developer
# can run `make quality` before pushing and reproduce the same
# pass/fail signal locally. Cycle v1.11.0.12.
#
# `make integration` (cycle v1.11.0.8) runs the real-Garage v1 + v2
# container suite via testcontainers-go. Docker required; CI runs
# the same via .github/workflows/integration.yml.

.DEFAULT_GOAL := help

GO        ?= go
PNPM      ?= pnpm
FRONTEND  ?= frontend

.PHONY: help
help:
	@echo "basement quality targets:"
	@echo "  make quality      — run lint + vet + test + sec + vulns (Go side)"
	@echo "  make lint         — golangci-lint run ./..."
	@echo "  make vet          — go vet ./..."
	@echo "  make test         — go test -race ./..."
	@echo "  make integration  — real-Garage v1+v2 + federation E2E (requires Docker)"
	@echo "  make sec          — gosec -conf .gosec.yml ./..."
	@echo "  make vulns        — govulncheck ./..."
	@echo "  make frontend     — pnpm lint + tsc --noEmit + test:run"
	@echo "  make smoke        — pnpm smoke (curated against deploy)"
	@echo "  make smoke-full   — pnpm smoke:full (comprehensive walk)"
	@echo "  make build        — go build ./... + pnpm build"

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

# Real-Garage container integration suite (cycle v1.11.0.8). Gated
# behind the `integration` Go build tag so `make test` / `go test ./...`
# stays Docker-free. Tests auto-skip on hosts without Docker so a
# laptop without the daemon produces a clean "skip" rather than a
# noisy fail. Bug-class coverage:
#   - v1.11.0.1 (admin-only Garage v2 driver constructed OK)
#   - v1.11.0.2 (per-cluster bucket handler ID round-trip)
#   - v1.11.0.5 BUG02 (Garage v2 grant readback after AllowBucketKey)
#   - v1.11.0.4 (federation engine no-op replication with whole-second mtimes)
.PHONY: integration
integration:
	$(GO) test -tags=integration -race -timeout=15m ./internal/drivers/garage/... ./internal/drivers/garage_v1/... ./internal/federation/...

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
