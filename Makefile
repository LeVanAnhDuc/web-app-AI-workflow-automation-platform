.PHONY: help db-up db-down migrate api worker web test test-go test-web build tidy fmt

help:
	@echo "db-up      start Postgres"
	@echo "migrate    apply migrations"
	@echo "api        run the API on :8080"
	@echo "worker     run the execution worker"
	@echo "web        run Next.js on :3000"
	@echo "test       run every test suite"

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
	cd web && npm run dev

build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	cd web && npm run build

test: test-go test-web

test-go:
	go test ./...

test-web:
	cd web && npm run typecheck && npm test

tidy:
	go mod tidy

fmt:
	go fmt ./...
