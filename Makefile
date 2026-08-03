# ai-cloud-ops Makefile
# All test / lint / build commands consolidated for CI + local dev.

.PHONY: help test test-race test-frontend test-all lint build verify clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

# --- Go backend ---

test: ## Run Go tests (no race detector, fast)
	go test -count=1 ./...

test-race: ## Run Go tests with race detector (CI gate, must be 0 race)
	go test -count=1 -race ./...

test-race-pkg: ## Run race tests for one package: make test-race-pkg PKG=./internal/api
	go test -count=1 -race -run 'Concurrent|Race|HighContention' $(PKG)

lint: ## Go lint (gofmt + go vet)
	gofmt -l .
	go vet ./...

build: ## Go build
	go build ./...

verify: lint test-race ## Full verification (CI gate)

check-doc-size: ## Warn if any docs file approaches/exceeds 500 lines
	@echo "Checking doc sizes (target: ≤ 500 lines per file)..."
	@for f in AGENTS.md docs/*.md; do \
		n=$$(wc -l < $$f); \
		if [ $$n -gt 500 ]; then \
			echo "  ❌ $$f: $$n lines (>500, MUST trim)"; \
		elif [ $$n -gt 400 ]; then \
			echo "  ⚠️  $$f: $$n lines (>400, consider trimming)"; \
		else \
			echo "  ✅ $$f: $$n lines"; \
		fi; \
	done

# --- Frontend ---

test-frontend: ## Run frontend tests (must be in web/)
	cd web && npx vitest run --no-coverage

test-frontend-watch: ## Frontend tests in watch mode
	cd web && npx vitest

tsc-check: ## TypeScript compile check
	cd web && npx tsc --noEmit

# --- Combined ---

test-all: test-race test-frontend tsc-check ## Run ALL tests (Go race + frontend + tsc)

# --- DB ---

migrate-up: ## Apply DB migrations
	@echo "Run via docker-compose or psql -f db/migrations/*.sql"

# --- Cleanup ---

clean: ## Remove build artifacts
	go clean ./...
	rm -rf web/.next web/node_modules/.cache