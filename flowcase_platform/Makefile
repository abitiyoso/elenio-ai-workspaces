SHELL := /bin/bash
PG_PORT ?= 55434
LOCAL_DB := postgres://flowcase:flowcase-local-only@127.0.0.1:$(PG_PORT)/flowcase?sslmode=disable
FLOWCASE_TEST_DATABASE_URL ?= $(LOCAL_DB)

.PHONY: deploy-vm help db-up db-down migrate seed-dev run-api run-worker test test-integration lint fmt-check \
        web-install web-typecheck web-lint web-test web-build check ci

help:
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

db-up: ## Start local PostgreSQL (docker compose)
	docker compose up -d --wait postgres

db-down: ## Stop local PostgreSQL
	docker compose stop postgres

migrate: ## Apply migrations to DATABASE_URL (defaults to local compose DB)
	DATABASE_URL=$${DATABASE_URL:-$(LOCAL_DB)} go run ./cmd/migrate up

seed-dev: ## Seed the fixed development identity (requires dev_mock env, see .env.example)
	go run ./cmd/migrate seed-dev

run-api: ## Run the API with the current environment
	go run ./cmd/api

run-worker: ## Run the worker with the current environment
	go run ./cmd/worker

test: ## Go unit tests (integration tests skip without a database)
	go test ./...

test-integration: ## Go unit + PostgreSQL integration tests; fails if the DB is unreachable
	FLOWCASE_REQUIRE_DB=1 FLOWCASE_TEST_DATABASE_URL='$(FLOWCASE_TEST_DATABASE_URL)' go test -count=1 -p 1 ./...

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint: fmt-check ## gofmt + go vet
	go vet ./...

web-install:
	cd web && npm ci

web-typecheck: ## Frontend TypeScript check
	cd web && npm run typecheck

web-lint: ## Frontend ESLint
	cd web && npm run lint

web-test: ## Frontend unit tests
	cd web && npm test

web-build: ## Frontend production build
	cd web && npm run build

check: lint test-integration web-typecheck web-lint web-test web-build ## Everything CI runs
ci: check

deploy-vm: ## Deploy the tailnet-only dev stack to kibi-vm (deploy/deploy-vm.sh)
	deploy/deploy-vm.sh $(VM)
