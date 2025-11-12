go_files := $(shell go list ./...)

.PHONY: all deps fmt lint vet test cover run-api run-worker migrate docker-up docker-down ci-cover

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
