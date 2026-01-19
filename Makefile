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

# Run full E2E test (requires API running)
e2e:
	./scripts/run-e2e-test.sh

# Run E2E test with shorter wait times
e2e-quick:
	./scripts/run-e2e-test.sh --quick
