# Ducker Flow Grid (web-app-AI-workflow-automation-platform)

> **Known debt:** `go.mod` still declares
> `module github.com/LeVanAnhDuc/app-AI-workflow-automation-platform` — the slug the
> repo carried before it was renamed on 2026-08-20. Builds still resolve through
> GitHub's rename redirect. Fixing it means rewriting the module path plus the
> imports in 79 `.go` files, and verifying that needs Go 1.25 (`go.mod` requires
> 1.25.7); do not attempt it on a toolchain that cannot compile the module.

A visual workflow automation platform: users draw a graph of nodes on a canvas and the platform
runs it — by hand, by webhook, or on a cron schedule — recording every run node by node so it can
be inspected, replayed and resumed. Go backend (`cmd/api` REST + SSE + webhook ingress,
`cmd/worker` execution engine) over Postgres, with a Next.js + TypeScript frontend in `web/`.

## Commands

```bash
cp .env.example .env    # then change JWT_SECRET and CREDENTIAL_KEY
make db-up              # start Postgres on :6543 (docker compose)
make db-down            # stop it
make migrate            # apply goose migrations (go run ./cmd/api -migrate-only)
make api                # run the API on :8080
make worker             # run the execution worker (second shell)
make web                # run Next.js on :3000, proxying /api and /webhook
make build              # go build ./cmd/api ./cmd/worker into bin/, plus next build
make test               # test-go + test-web
make test-go            # go test ./...
make test-web           # cd web && pnpm typecheck && pnpm test (Vitest)
make test-e2e           # Playwright; needs Postgres, cmd/api and cmd/worker running
make fmt                # go fmt ./...
make tidy               # go mod tidy
```

The store, queue and integration suites skip without a database; set
`TEST_DATABASE_URL=postgres://flowgrid:flowgrid@localhost:6543/flowgrid?sslmode=disable` to run them.

## README (REQUIRED — keep in sync with features)

`README.md` describes what the platform does for its users — it is not a boilerplate page. Every commit that adds or changes user-facing behaviour MUST update the `## Features` section of `README.md` in the same commit, before merging — one short English bullet in the existing style.

While touching README, refresh any stale numbers you notice (test counts, stack versions).
