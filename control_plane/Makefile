TEST_DB ?= postgres://cp:cp-test-only@127.0.0.1:55436/cp_test?sslmode=disable

.PHONY: check test test-integration web-check db-up deploy-vm
check: test-integration web-check
	gofmt -l cmd internal tests migrations | (! grep .)
	go vet ./cmd/... ./internal/... ./tests/...

test:
	go test ./cmd/... ./internal/...

test-integration:
	CP_TEST_DATABASE_URL='$(TEST_DB)' CP_REQUIRE_DB=1 go test -count=1 ./cmd/... ./internal/... ./tests/...

web-check:
	cd web && npm run typecheck && npm run lint && npm test && npm run build

db-up:
	docker run -d --name cp-test-pg -e POSTGRES_PASSWORD=cp-test-only -e POSTGRES_USER=cp -e POSTGRES_DB=cp_test -p 127.0.0.1:55436:5432 postgres:16-alpine

deploy-vm:
	deploy/deploy-vm.sh $(VM)
