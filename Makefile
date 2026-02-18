go_files := $(shell go list ./...)

.PHONY: all deps fmt lint vet test cover run-api run-worker migrate docker-up docker-down ci-cover regression
.PHONY: clear-test-deposit e2e e2e-quick

all: test

deps:
	go mod download
	go mod verify

fmt:
	gofmt -s -w .

lint: fmt
	go vet ./...
	golangci-lint run || true

vet:
	go vet ./...

migrate:
	migrate -path migrations -database $$DATABASE_URL up

cover:
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

ci-cover:
	bash scripts/check_package_coverage.sh

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/messaging-worker

docker-up:
	docker compose up --build

docker-down:
	docker compose down -v

test:
	go test ./...

regression:
	go test ./tests -run Regression -v

# Clears deposit state for a test phone (dry-run unless YES=1).
clear-test-deposit:
	./scripts/clear-test-deposit.sh $(TEST_PHONE)

# Run full E2E test against dev API (requires ADMIN_JWT_SECRET and API_BASE_URL env vars)
e2e:
	ADMIN_JWT_SECRET=$(ADMIN_JWT_SECRET) API_BASE_URL=$(API_BASE_URL) go run scripts/e2e/run_e2e.go

# Config-driven service tests (all services from clinic config)
# Usage: make test-services ORG=<orgID> [TIER=1|2|3]
test-services:
	@test -n "$$ADMIN_JWT_SECRET" || { echo "Set ADMIN_JWT_SECRET env var"; exit 1; }
	go run scripts/e2e/service_tests.go \
		--org=$(ORG) \
		--tier=$(or $(TIER),1) \
		--secret=$$ADMIN_JWT_SECRET

# Run E2E test with default dev settings
# Requires: ADMIN_JWT_SECRET env var (see .env.example)
e2e-dev:
	@test -n "$$ADMIN_JWT_SECRET" || { echo "Set ADMIN_JWT_SECRET env var"; exit 1; }
	API_BASE_URL=https://api-dev.aiwolfsolutions.com go run scripts/e2e/run_e2e.go
