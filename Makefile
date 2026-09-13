.PHONY: help db-up db-down migrate api worker web test test-go test-web test-e2e build tidy fmt

help:
	@echo "db-up      start Postgres"
	@echo "migrate    apply migrations"
	@echo "api        run the API on :8080"
	@echo "worker     run the execution worker"
	@echo "web        run Next.js on :3000"
	@echo "test       run every test suite"
	@echo "test-e2e   drive the running app in a browser"

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

migrate:
	go run ./cmd/api -migrate-only

api:
	go run ./cmd/api

worker:
	go run ./cmd/worker

web:
	cd web && pnpm dev

build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	cd web && pnpm build

test: test-go test-web

test-go:
	go test ./...

test-web:
	cd web && pnpm typecheck && pnpm test

# Needs Postgres, the API and the worker already running.
test-e2e:
	cd web && pnpm test:e2e

tidy:
	go mod tidy

fmt:
	go fmt ./...
